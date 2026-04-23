package crypto

import (
	"crypto/ed25519"
	"testing"
)

func TestSignRequest_RoundTrip(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	keys := testTenantKeys(t, km)

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/cart/add",
		Body:      []byte(`{"item":"sku-123","qty":1}`),
		Timestamp: "1700000010",
		Nonce:     "nonce-sign-1",
	}

	sig, err := km.SignRequest(ctx, keys, payload)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	// Verify with the public key.
	msg := payload.CanonicalMessage()
	if !ed25519.Verify(keys.ArxSigningPublicKey, msg, sig) {
		t.Error("signature verification failed with the matching public key")
	}
}

func TestSignRequest_DeterministicEd25519(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	keys := testTenantKeys(t, km)

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/cart/add",
		Body:      []byte(`{"item":"sku-123"}`),
		Timestamp: "1700000020",
		Nonce:     "nonce-sign-2",
	}

	sig1, err := km.SignRequest(ctx, keys, payload)
	if err != nil {
		t.Fatal(err)
	}

	sig2, err := km.SignRequest(ctx, keys, payload)
	if err != nil {
		t.Fatal(err)
	}

	// Ed25519 is deterministic — same key + same message = same signature.
	if string(sig1) != string(sig2) {
		t.Error("expected deterministic signatures for same payload and key")
	}
}

func TestSignRequest_CrossTenantIsolation(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	keys1 := testTenantKeys(t, km)
	keys2 := testTenantKeys(t, km)

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/orders/list",
		Body:      []byte(`{}`),
		Timestamp: "1700000030",
		Nonce:     "nonce-sign-3",
	}

	sig1, err := km.SignRequest(ctx, keys1, payload)
	if err != nil {
		t.Fatal(err)
	}

	// Signature from tenant 1 should NOT verify with tenant 2's public key.
	msg := payload.CanonicalMessage()
	if ed25519.Verify(keys2.ArxSigningPublicKey, msg, sig1) {
		t.Error("signature from tenant 1 should not verify with tenant 2's public key")
	}
}

func TestSignRequest_WrongDEK(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	keys := testTenantKeys(t, km)

	// Replace the DEK with a different one — should fail to decrypt the signing key.
	wrongDEK, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys.DEKEnc = wrongDEK

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/test",
		Body:      []byte(`{}`),
		Timestamp: "1700000040",
		Nonce:     "nonce-sign-4",
	}

	_, err = km.SignRequest(ctx, keys, payload)
	if err == nil {
		t.Error("expected error when DEK doesn't match the encrypted signing key")
	}
}
