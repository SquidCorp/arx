package proxy_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fambr/arx/internal/proxy"
)

// fakeSigner implements proxy.RequestSigner for tests using a raw Ed25519 key.
type fakeSigner struct {
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
}

func newFakeSigner(t *testing.T) *fakeSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeSigner{privKey: priv, pubKey: pub}
}

func (s *fakeSigner) Sign(_ context.Context, _ string, payload proxy.SignPayload) ([]byte, error) {
	msg := payload.Method + "\n" + payload.Path + "\n" + payload.Timestamp + "\n" + payload.Nonce + "\n"
	msg2 := append([]byte(msg), payload.Body...)
	return ed25519.Sign(s.privKey, msg2), nil
}

// failingSigner always returns an error.
type failingSigner struct{}

func (s *failingSigner) Sign(_ context.Context, _ string, _ proxy.SignPayload) ([]byte, error) {
	return nil, errors.New("signing unavailable")
}

func TestCaller_SignsOutboundRequests(t *testing.T) {
	t.Parallel()

	signer := newFakeSigner(t)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	var capturedHeaders http.Header
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:cart-add": {
			UpstreamURL:    srv.URL + "/cart/add",
			UpstreamMethod: "POST",
			ParamMapping:   `{"product_id":"body.item_id","quantity":"body.qty"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver,
		proxy.WithSigner(signer),
		proxy.WithNow(func() time.Time { return fixedTime }),
	)

	_, err := caller.CallTool(
		context.Background(),
		"t1", "cart-add",
		map[string]any{"product_id": "SKU-42", "quantity": 3},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify X-Arx-Signature header is present and base64-decodable.
	sigHeader := capturedHeaders.Get(proxy.HeaderSignature)
	if sigHeader == "" {
		t.Fatal("missing X-Arx-Signature header")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigHeader)
	if err != nil {
		t.Fatalf("X-Arx-Signature is not valid base64: %v", err)
	}

	// Verify X-Arx-Timestamp header matches fixed time.
	tsHeader := capturedHeaders.Get(proxy.HeaderTimestamp)
	if tsHeader == "" {
		t.Fatal("missing X-Arx-Timestamp header")
	}
	if tsHeader != "1736942400" {
		t.Errorf("expected timestamp 1736942400, got %s", tsHeader)
	}

	// Verify X-Arx-Nonce header is present.
	nonceHeader := capturedHeaders.Get(proxy.HeaderNonce)
	if nonceHeader == "" {
		t.Fatal("missing X-Arx-Nonce header")
	}
	if len(nonceHeader) != 32 { // 16 bytes hex-encoded
		t.Errorf("expected 32-char hex nonce, got %d chars", len(nonceHeader))
	}

	// Verify the signature is valid.
	msg := "POST\n/cart/add\n" + tsHeader + "\n" + nonceHeader + "\n"
	canonical := append([]byte(msg), capturedBody...)
	if !ed25519.Verify(signer.pubKey, canonical, sigBytes) {
		t.Error("signature verification failed")
	}
}

func TestCaller_SignsGETRequestsWithoutBody(t *testing.T) {
	t.Parallel()

	signer := newFakeSigner(t)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	var capturedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []string{}})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:search": {
			UpstreamURL:    srv.URL + "/products/search",
			UpstreamMethod: "GET",
			ParamMapping:   `{"category":"query.cat"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver,
		proxy.WithSigner(signer),
		proxy.WithNow(func() time.Time { return fixedTime }),
	)

	_, err := caller.CallTool(
		context.Background(),
		"t1", "search",
		map[string]any{"category": "electronics"},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Signature headers must be present even for GET requests.
	if capturedHeaders.Get(proxy.HeaderSignature) == "" {
		t.Error("missing X-Arx-Signature on GET request")
	}
	if capturedHeaders.Get(proxy.HeaderTimestamp) == "" {
		t.Error("missing X-Arx-Timestamp on GET request")
	}
	if capturedHeaders.Get(proxy.HeaderNonce) == "" {
		t.Error("missing X-Arx-Nonce on GET request")
	}

	// Verify the signature covers the query string in the path.
	sigBytes, _ := base64.StdEncoding.DecodeString(capturedHeaders.Get(proxy.HeaderSignature))
	tsHeader := capturedHeaders.Get(proxy.HeaderTimestamp)
	nonceHeader := capturedHeaders.Get(proxy.HeaderNonce)

	msg := "GET\n/products/search?cat=electronics\n" + tsHeader + "\n" + nonceHeader + "\n"
	canonical := []byte(msg)
	if !ed25519.Verify(signer.pubKey, canonical, sigBytes) {
		t.Error("GET request signature verification failed")
	}
}

func TestCaller_SigningErrorFailsCall(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:tool": {
			UpstreamURL:    srv.URL + "/api",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver, proxy.WithSigner(&failingSigner{}))

	_, err := caller.CallTool(
		context.Background(),
		"t1", "tool",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err == nil {
		t.Fatal("expected error when signer fails")
	}
	if !strings.Contains(err.Error(), "sign request") {
		t.Errorf("expected 'sign request' in error, got: %v", err)
	}
}

func TestCaller_NoSignerSkipsSigning(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:tool": {
			UpstreamURL:    srv.URL + "/api",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	// No WithSigner — signing should be skipped.
	caller := proxy.NewCaller(resolver)
	_, err := caller.CallTool(
		context.Background(),
		"t1", "tool",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeaders.Get(proxy.HeaderSignature) != "" {
		t.Error("X-Arx-Signature should not be present without a signer")
	}
	if capturedHeaders.Get(proxy.HeaderTimestamp) != "" {
		t.Error("X-Arx-Timestamp should not be present without a signer")
	}
}

func TestCaller_SignatureBodyPreservedForUpstream(t *testing.T) {
	t.Parallel()

	signer := newFakeSigner(t)

	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:cart-add": {
			UpstreamURL:    srv.URL + "/cart/add",
			UpstreamMethod: "POST",
			ParamMapping:   `{"product_id":"body.item_id"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver, proxy.WithSigner(signer))

	_, err := caller.CallTool(
		context.Background(),
		"t1", "cart-add",
		map[string]any{"product_id": "SKU-99"},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The upstream must still receive the full body after signing consumed it.
	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("upstream did not receive valid JSON body: %v", err)
	}
	if body["item_id"] != "SKU-99" {
		t.Errorf("expected item_id=SKU-99, got %v", body["item_id"])
	}
}

func TestCaller_UniqueNoncePerRequest(t *testing.T) {
	t.Parallel()

	signer := newFakeSigner(t)

	var nonces []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonces = append(nonces, r.Header.Get(proxy.HeaderNonce))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:tool": {
			UpstreamURL:    srv.URL + "/api",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver, proxy.WithSigner(signer))

	for i := 0; i < 5; i++ {
		_, err := caller.CallTool(
			context.Background(),
			"t1", "tool",
			map[string]any{},
			httptest.NewRequest(http.MethodPost, "/mcp", nil),
		)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	seen := make(map[string]bool)
	for _, n := range nonces {
		if seen[n] {
			t.Errorf("duplicate nonce: %s", n)
		}
		seen[n] = true
	}
}
