package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

// LocalKeyManager implements KeyManager using a local master key from the environment.
// It uses AES-256-GCM for symmetric encryption and Ed25519 for signing and verification.
type LocalKeyManager struct {
	masterKey []byte
}

// NewLocalKeyManager creates a LocalKeyManager from a hex-encoded 32-byte master key.
// It returns an error if the key is missing, not valid hex, or not exactly 32 bytes.
func NewLocalKeyManager(hexKey string) (*LocalKeyManager, error) {
	if hexKey == "" {
		return nil, errors.New("master key is required (ARX_MASTER_KEY)")
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("master key must be valid hex: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}

	return &LocalKeyManager{masterKey: key}, nil
}

// EncryptSessionData encrypts plaintext session data using the tenant's DEK.
func (km *LocalKeyManager) EncryptSessionData(_ context.Context, dekEnc []byte, plaintext []byte) ([]byte, error) {
	dek, err := km.decryptAESGCM(km.masterKey, dekEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK: %w", err)
	}
	defer zeroBytes(dek)

	return km.encryptAESGCM(dek, plaintext)
}

// DecryptSessionData decrypts ciphertext session data using the tenant's DEK.
func (km *LocalKeyManager) DecryptSessionData(_ context.Context, dekEnc []byte, ciphertext []byte) ([]byte, error) {
	dek, err := km.decryptAESGCM(km.masterKey, dekEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK: %w", err)
	}
	defer zeroBytes(dek)

	return km.decryptAESGCM(dek, ciphertext)
}

// SignRequest signs a payload using the tenant's encrypted Ed25519 signing key.
// The decrypted private key is zeroed after use.
func (km *LocalKeyManager) SignRequest(_ context.Context, keys TenantKeys, payload SignedPayload) ([]byte, error) {
	dek, err := km.decryptAESGCM(km.masterKey, keys.DEKEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK: %w", err)
	}
	defer zeroBytes(dek)

	privKey, err := km.decryptAESGCM(dek, keys.ArxSigningPrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt signing key: %w", err)
	}
	defer zeroBytes(privKey)

	msg := payload.CanonicalMessage()
	sig := ed25519.Sign(ed25519.PrivateKey(privKey), msg)

	return sig, nil
}

// VerifyWebhook verifies an Ed25519 signature against the merchant's public key(s).
// It tries the current key first, then falls back to the previous key if within the grace period.
func (km *LocalKeyManager) VerifyWebhook(_ context.Context, keys TenantKeys, payload SignedPayload, signature []byte) error {
	msg := payload.CanonicalMessage()

	// Try current key first.
	if ed25519.Verify(keys.MerchantPublicKey, msg, signature) {
		return nil
	}

	// Fall back to previous key if within grace period.
	if keys.MerchantPublicKeyPrevious != nil && keys.MerchantKeyGraceUntil != nil {
		if time.Now().Before(*keys.MerchantKeyGraceUntil) {
			if ed25519.Verify(keys.MerchantPublicKeyPrevious, msg, signature) {
				return nil
			}
		}
	}

	return errors.New("invalid_signature")
}

// GenerateDEK generates a random AES-256 DEK and returns it encrypted with the master key.
func (km *LocalKeyManager) GenerateDEK(_ context.Context) ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}
	defer zeroBytes(dek)

	return km.encryptAESGCM(km.masterKey, dek)
}

// GenerateSigningKeypair generates a new Ed25519 keypair. The private key is encrypted
// with the tenant's DEK (which is itself encrypted with the master key).
func (km *LocalKeyManager) GenerateSigningKeypair(_ context.Context, dekEnc []byte) (ed25519.PublicKey, []byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate Ed25519 keypair: %w", err)
	}
	defer zeroBytes(priv)

	dek, err := km.decryptAESGCM(km.masterKey, dekEnc)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt DEK: %w", err)
	}
	defer zeroBytes(dek)

	privEnc, err := km.encryptAESGCM(dek, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt signing key: %w", err)
	}

	return pub, privEnc, nil
}

// encryptAESGCM encrypts plaintext with a 32-byte key using AES-256-GCM.
// Returns nonce || ciphertext.
func (km *LocalKeyManager) encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptAESGCM decrypts data encrypted by encryptAESGCM (nonce || ciphertext).
func (km *LocalKeyManager) decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]

	return gcm.Open(nil, nonce, ct, nil)
}

// zeroBytes overwrites a byte slice with zeros.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
