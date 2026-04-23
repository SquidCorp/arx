package crypto

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestRotateSigningKey_GeneratesNewKeypair(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	keys := testTenantKeys(t, km)

	oldPub := keys.ArxSigningPublicKey
	gracePeriod := 24 * time.Hour

	result, err := km.RotateSigningKey(ctx, keys, gracePeriod)
	if err != nil {
		t.Fatalf("rotate failed: %v", err)
	}

	// New public key should differ from old.
	if result.NewPublicKey.Equal(oldPub) {
		t.Error("new public key should differ from old")
	}

	// New private key should be encrypted.
	if len(result.NewPrivateKeyEnc) == 0 {
		t.Error("new encrypted private key should not be empty")
	}

	// Old key record should be populated.
	if !result.OldPublicKey.Equal(oldPub) {
		t.Error("old public key in result should match original")
	}

	if result.OldKeyActiveUntil.IsZero() {
		t.Error("old key active_until should be set")
	}

	// Grace period should be approximately now + gracePeriod.
	expectedUntil := time.Now().Add(gracePeriod)
	diff := result.OldKeyActiveUntil.Sub(expectedUntil)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("grace period off: expected ~%v, got %v", expectedUntil, result.OldKeyActiveUntil)
	}
}

func TestRotateSigningKey_NewKeyCanSign(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	keys := testTenantKeys(t, km)

	result, err := km.RotateSigningKey(ctx, keys, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Build new TenantKeys with the rotated key.
	newKeys := TenantKeys{
		ArxSigningPublicKey:     result.NewPublicKey,
		ArxSigningPrivateKeyEnc: result.NewPrivateKeyEnc,
		DEKEnc:                  keys.DEKEnc,
	}

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/test",
		Body:      []byte(`{"rotated":true}`),
		Timestamp: "1700000050",
		Nonce:     "nonce-rotate-1",
	}

	sig, err := km.SignRequest(ctx, newKeys, payload)
	if err != nil {
		t.Fatalf("sign with new key failed: %v", err)
	}

	if !ed25519.Verify(result.NewPublicKey, payload.CanonicalMessage(), sig) {
		t.Error("signature from new key should verify with new public key")
	}
}

func TestRotateSigningKey_OldKeyStillVerifies(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	keys := testTenantKeys(t, km)

	payload := SignedPayload{
		Method:    "POST",
		Path:      "/api/test",
		Body:      []byte(`{"before":"rotation"}`),
		Timestamp: "1700000060",
		Nonce:     "nonce-rotate-2",
	}

	// Sign with old key before rotation.
	sigOld, err := km.SignRequest(ctx, keys, payload)
	if err != nil {
		t.Fatal(err)
	}

	// Verify old signature still works with old public key.
	if !ed25519.Verify(keys.ArxSigningPublicKey, payload.CanonicalMessage(), sigOld) {
		t.Error("old signature should still verify with old public key")
	}

	// After rotation, the old public key should NOT verify a NEW signature.
	result, err := km.RotateSigningKey(ctx, keys, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	newKeys := TenantKeys{
		ArxSigningPublicKey:     result.NewPublicKey,
		ArxSigningPrivateKeyEnc: result.NewPrivateKeyEnc,
		DEKEnc:                  keys.DEKEnc,
	}

	sigNew, err := km.SignRequest(ctx, newKeys, payload)
	if err != nil {
		t.Fatal(err)
	}

	if ed25519.Verify(keys.ArxSigningPublicKey, payload.CanonicalMessage(), sigNew) {
		t.Error("new signature should NOT verify with old public key")
	}
}

func TestRotateMerchantKey(t *testing.T) {
	oldPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	newPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gracePeriod := 24 * time.Hour
	result := RotateMerchantKey(oldPub, newPub, gracePeriod)

	if !result.NewPublicKey.Equal(newPub) {
		t.Error("new public key should be the provided key")
	}

	if !result.OldPublicKey.Equal(oldPub) {
		t.Error("old public key should be the previous current key")
	}

	expectedGrace := time.Now().Add(gracePeriod)
	diff := result.GraceUntil.Sub(expectedGrace)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("grace period off: expected ~%v, got %v", expectedGrace, result.GraceUntil)
	}
}
