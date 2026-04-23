package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestNewLocalKeyManager_ValidKey(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	hexKey := hex.EncodeToString(key)

	km, err := NewLocalKeyManager(hexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if km == nil {
		t.Fatal("expected non-nil KeyManager")
	}
}

func TestNewLocalKeyManager_EmptyKey(t *testing.T) {
	_, err := NewLocalKeyManager("")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewLocalKeyManager_InvalidHex(t *testing.T) {
	_, err := NewLocalKeyManager("not-hex-at-all!")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestNewLocalKeyManager_WrongLength(t *testing.T) {
	// 16 bytes instead of 32
	shortKey := hex.EncodeToString(make([]byte, 16))
	_, err := NewLocalKeyManager(shortKey)
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestLocalKeyManager_ImplementsKeyManager(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	km, err := NewLocalKeyManager(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}

	// Compile-time check that LocalKeyManager implements KeyManager.
	var _ KeyManager = km
}

func testMasterKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(key)
}

func testTenantKeys(t *testing.T, km KeyManager) TenantKeys {
	t.Helper()
	ctx := t.Context()

	dekEnc, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pub, privEnc, err := km.GenerateSigningKeypair(ctx, dekEnc)
	if err != nil {
		t.Fatal(err)
	}

	// Generate a merchant keypair for webhook verification tests.
	merchantPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return TenantKeys{
		MerchantPublicKey:       merchantPub,
		ArxSigningPublicKey:     pub,
		ArxSigningPrivateKeyEnc: privEnc,
		DEKEnc:                  dekEnc,
	}
}
