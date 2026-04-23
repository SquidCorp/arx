// Package crypto provides cryptographic operations for tenant key management,
// data encryption, webhook signature verification, and outbound request signing.
package crypto

import (
	"context"
	"crypto/ed25519"
	"time"
)

// TenantKeys holds the cryptographic material for a single tenant.
type TenantKeys struct {
	// MerchantPublicKey is the merchant's current Ed25519 public key for webhook verification.
	MerchantPublicKey ed25519.PublicKey
	// MerchantPublicKeyPrevious is the merchant's previous public key, valid during grace period.
	MerchantPublicKeyPrevious ed25519.PublicKey
	// MerchantKeyGraceUntil is the deadline after which the previous key is rejected.
	MerchantKeyGraceUntil *time.Time

	// ArxSigningPublicKey is the Arx Ed25519 public key for outbound request signing.
	ArxSigningPublicKey ed25519.PublicKey
	// ArxSigningPrivateKeyEnc is the Arx signing private key encrypted with the tenant DEK.
	ArxSigningPrivateKeyEnc []byte

	// DEKEnc is the tenant's data encryption key, encrypted with the master key.
	DEKEnc []byte
}

// SignedPayload holds the data to be signed or verified for webhook/request signatures.
type SignedPayload struct {
	// Method is the HTTP method (e.g., "POST").
	Method string
	// Path is the request path (e.g., "/webhook/session-connected").
	Path string
	// Body is the raw request body.
	Body []byte
	// Timestamp is the request timestamp as a Unix epoch string.
	Timestamp string
	// Nonce is a unique value to prevent replay attacks.
	Nonce string
}

// CanonicalMessage builds the canonical byte representation used for signing and verification.
func (sp SignedPayload) CanonicalMessage() []byte {
	// Format: method\npath\ntimestamp\nnonce\nbody
	msg := sp.Method + "\n" + sp.Path + "\n" + sp.Timestamp + "\n" + sp.Nonce + "\n"
	return append([]byte(msg), sp.Body...)
}

// KeyManager abstracts all encryption and signing operations.
// The v1 implementation uses a local master key; v2 can swap in a KMS-backed implementation.
type KeyManager interface {
	// EncryptSessionData encrypts session claims using the tenant's DEK.
	EncryptSessionData(ctx context.Context, dekEnc []byte, plaintext []byte) ([]byte, error)

	// DecryptSessionData decrypts session claims using the tenant's DEK.
	DecryptSessionData(ctx context.Context, dekEnc []byte, ciphertext []byte) ([]byte, error)

	// SignRequest signs an outbound request payload using the tenant's encrypted signing key.
	SignRequest(ctx context.Context, keys TenantKeys, payload SignedPayload) ([]byte, error)

	// VerifyWebhook verifies an inbound webhook signature against the merchant's public key(s).
	// It tries the current key first, then falls back to the previous key during grace period.
	VerifyWebhook(ctx context.Context, keys TenantKeys, payload SignedPayload, signature []byte) error

	// GenerateDEK generates a new AES-256 data encryption key and returns it encrypted with the master key.
	GenerateDEK(ctx context.Context) ([]byte, error)

	// GenerateSigningKeypair generates a new Ed25519 signing keypair.
	// Returns the public key and the private key encrypted with the given DEK.
	GenerateSigningKeypair(ctx context.Context, dekEnc []byte) (publicKey ed25519.PublicKey, privateKeyEnc []byte, err error)
}
