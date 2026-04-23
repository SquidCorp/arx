package crypto

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"
)

// SigningKeyRotation holds the result of rotating an Arx signing keypair.
type SigningKeyRotation struct {
	// NewPublicKey is the newly generated Ed25519 public key.
	NewPublicKey ed25519.PublicKey
	// NewPrivateKeyEnc is the new private key encrypted with the tenant's DEK.
	NewPrivateKeyEnc []byte
	// OldPublicKey is the previous public key that should be recorded in tenant_keys.
	OldPublicKey ed25519.PublicKey
	// OldKeyActiveUntil is the deadline after which the old key expires (grace period end).
	OldKeyActiveUntil time.Time
}

// MerchantKeyRotation holds the result of rotating a merchant's public key.
type MerchantKeyRotation struct {
	// NewPublicKey is the new merchant Ed25519 public key.
	NewPublicKey ed25519.PublicKey
	// OldPublicKey is the previous merchant public key (kept during grace period).
	OldPublicKey ed25519.PublicKey
	// GraceUntil is the deadline after which the old key is rejected.
	GraceUntil time.Time
}

// RotateSigningKey generates a new Ed25519 signing keypair for the tenant.
// The old key is recorded with a grace period. The caller is responsible for
// persisting the result to the tenants and tenant_keys tables.
func (km *LocalKeyManager) RotateSigningKey(ctx context.Context, keys TenantKeys, gracePeriod time.Duration) (*SigningKeyRotation, error) {
	newPub, newPrivEnc, err := km.GenerateSigningKeypair(ctx, keys.DEKEnc)
	if err != nil {
		return nil, fmt.Errorf("generate new signing keypair: %w", err)
	}

	return &SigningKeyRotation{
		NewPublicKey:      newPub,
		NewPrivateKeyEnc:  newPrivEnc,
		OldPublicKey:      keys.ArxSigningPublicKey,
		OldKeyActiveUntil: time.Now().Add(gracePeriod),
	}, nil
}

// RotateMerchantKey builds a MerchantKeyRotation from the current and new public keys.
// The caller is responsible for updating the tenant record and recording key history.
func RotateMerchantKey(currentPub, newPub ed25519.PublicKey, gracePeriod time.Duration) *MerchantKeyRotation {
	return &MerchantKeyRotation{
		NewPublicKey: newPub,
		OldPublicKey: currentPub,
		GraceUntil:   time.Now().Add(gracePeriod),
	}
}
