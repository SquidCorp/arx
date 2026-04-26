package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// HeaderSignature is the header name for the Ed25519 signature on outbound proxied requests.
const HeaderSignature = "X-Arx-Signature"

// HeaderTimestamp is the header name for the request timestamp on outbound proxied requests.
const HeaderTimestamp = "X-Arx-Timestamp"

// HeaderNonce is the header name for the request nonce on outbound proxied requests.
const HeaderNonce = "X-Arx-Nonce"

// SignPayload holds the fields that the RequestSigner must sign.
type SignPayload struct {
	// Method is the HTTP method (e.g., "POST").
	Method string
	// Path is the request path (e.g., "/cart/add").
	Path string
	// Body is the raw request body (nil for bodyless methods).
	Body []byte
	// Timestamp is the Unix epoch seconds as a string.
	Timestamp string
	// Nonce is a unique value to prevent replay attacks.
	Nonce string
}

// RequestSigner signs outbound proxy requests for a specific tenant.
type RequestSigner interface {
	// Sign signs the payload and returns the raw Ed25519 signature.
	Sign(ctx context.Context, tenantID string, payload SignPayload) ([]byte, error)
}

// signRequest signs an outbound HTTP request and injects the signature headers.
// It reads the request body for signing, then restores it for the HTTP client.
func signRequest(ctx context.Context, signer RequestSigner, tenantID string, req *http.Request, now func() time.Time) error {
	timestamp := strconv.FormatInt(now().Unix(), 10)

	nonce, err := generateNonce()
	if err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	// Read the body for signing (may be nil for GET requests).
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("read body for signing: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	payload := SignPayload{
		Method:    req.Method,
		Path:      path,
		Body:      body,
		Timestamp: timestamp,
		Nonce:     nonce,
	}

	sig, err := signer.Sign(ctx, tenantID, payload)
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	req.Header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(sig))
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderNonce, nonce)

	return nil
}

// generateNonce returns a cryptographically random 16-byte hex-encoded nonce.
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
