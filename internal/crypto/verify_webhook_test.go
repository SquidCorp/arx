package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestVerifyWebhook_CurrentKey(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}

	merchantPub, merchantPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	keys := TenantKeys{MerchantPublicKey: merchantPub}
	payload := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-connected",
		Body:      []byte(`{"sid":"s1"}`),
		Timestamp: "1700000000",
		Nonce:     "nonce-1",
	}

	sig := ed25519.Sign(merchantPriv, payload.CanonicalMessage())

	if err := km.VerifyWebhook(t.Context(), keys, payload, sig); err != nil {
		t.Errorf("expected valid signature, got: %v", err)
	}
}

func TestVerifyWebhook_PreviousKeyWithinGrace(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}

	// Old key (still in grace period).
	oldPub, oldPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// New key (current).
	newPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	graceUntil := time.Now().Add(24 * time.Hour)
	keys := TenantKeys{
		MerchantPublicKey:         newPub,
		MerchantPublicKeyPrevious: oldPub,
		MerchantKeyGraceUntil:     &graceUntil,
	}

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-refresh",
		Body:      []byte(`{"sid":"s2"}`),
		Timestamp: "1700000001",
		Nonce:     "nonce-2",
	}

	// Sign with the old key — should still verify during grace period.
	sig := ed25519.Sign(oldPriv, payload.CanonicalMessage())

	if err := km.VerifyWebhook(t.Context(), keys, payload, sig); err != nil {
		t.Errorf("expected valid signature with previous key during grace period, got: %v", err)
	}
}

func TestVerifyWebhook_PreviousKeyExpiredGrace(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}

	oldPub, oldPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	newPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Grace period already expired.
	graceUntil := time.Now().Add(-1 * time.Hour)
	keys := TenantKeys{
		MerchantPublicKey:         newPub,
		MerchantPublicKeyPrevious: oldPub,
		MerchantKeyGraceUntil:     &graceUntil,
	}

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-revoked",
		Body:      []byte(`{"sid":"s3"}`),
		Timestamp: "1700000002",
		Nonce:     "nonce-3",
	}

	sig := ed25519.Sign(oldPriv, payload.CanonicalMessage())

	err = km.VerifyWebhook(t.Context(), keys, payload, sig)
	if err == nil {
		t.Error("expected error for expired grace period")
	}
	if err.Error() != "invalid_signature" {
		t.Errorf("expected 'invalid_signature', got %q", err.Error())
	}
}

func TestVerifyWebhook_InvalidSignature(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}

	merchantPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Sign with a completely different key.
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	keys := TenantKeys{MerchantPublicKey: merchantPub}
	payload := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-connected",
		Body:      []byte(`{"sid":"s4"}`),
		Timestamp: "1700000003",
		Nonce:     "nonce-4",
	}

	sig := ed25519.Sign(wrongPriv, payload.CanonicalMessage())

	err = km.VerifyWebhook(t.Context(), keys, payload, sig)
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifyWebhook_TamperedBody(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}

	merchantPub, merchantPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	keys := TenantKeys{MerchantPublicKey: merchantPub}
	payload := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-connected",
		Body:      []byte(`{"sid":"original"}`),
		Timestamp: "1700000004",
		Nonce:     "nonce-5",
	}

	sig := ed25519.Sign(merchantPriv, payload.CanonicalMessage())

	// Tamper with the body.
	payload.Body = []byte(`{"sid":"tampered"}`)

	err = km.VerifyWebhook(t.Context(), keys, payload, sig)
	if err == nil {
		t.Error("expected error for tampered body")
	}
}
