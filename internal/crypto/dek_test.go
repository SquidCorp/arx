package crypto

import (
	"encoding/hex"
	"testing"
)

func TestGenerateDEK_Unique(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	dek1, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dek2, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(dek1) == hex.EncodeToString(dek2) {
		t.Error("two DEKs should not be identical")
	}
}

func TestEncryptDecryptSessionData_RoundTrip(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	dekEnc, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte(`{"loyaltyTier":"gold","userId":"u123"}`)

	ciphertext, err := km.EncryptSessionData(ctx, dekEnc, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := km.DecryptSessionData(ctx, dekEnc, ciphertext)
	if err != nil {
		t.Fatal(err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptSessionData_WrongDEK(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	dek1, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dek2, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret data")

	ciphertext, err := km.EncryptSessionData(ctx, dek1, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Decrypting with a different DEK should fail.
	_, err = km.DecryptSessionData(ctx, dek2, ciphertext)
	if err == nil {
		t.Error("expected error decrypting with wrong DEK")
	}
}

func TestDecryptSessionData_WrongMasterKey(t *testing.T) {
	km1, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	km2, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	dekEnc, err := km1.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret data")

	ciphertext, err := km1.EncryptSessionData(ctx, dekEnc, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Different master key cannot unwrap the DEK.
	_, err = km2.DecryptSessionData(ctx, dekEnc, ciphertext)
	if err == nil {
		t.Error("expected error decrypting with wrong master key")
	}
}

func TestEncryptSessionData_DifferentCiphertextEachTime(t *testing.T) {
	km, err := NewLocalKeyManager(testMasterKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	dekEnc, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("same data")

	ct1, err := km.EncryptSessionData(ctx, dekEnc, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	ct2, err := km.EncryptSessionData(ctx, dekEnc, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// GCM uses random nonces, so ciphertexts must differ.
	if hex.EncodeToString(ct1) == hex.EncodeToString(ct2) {
		t.Error("encrypting the same plaintext twice should produce different ciphertexts")
	}
}
