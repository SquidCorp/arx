// Package main provides a CLI tool to seed a test tenant with known Ed25519 keys
// for use with Bruno e2e tests.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	masterKeyHex := os.Getenv("ARX_MASTER_KEY")
	if masterKeyHex == "" {
		return fmt.Errorf("ARX_MASTER_KEY env var is required")
	}

	dbURL := os.Getenv("APP_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/arx?sslmode=disable"
	}

	km, err := crypto.NewLocalKeyManager(masterKeyHex)
	if err != nil {
		return fmt.Errorf("create key manager: %w", err)
	}

	ctx := context.Background()

	// Generate merchant Ed25519 keypair.
	merchantPub, merchantPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate merchant keypair: %w", err)
	}

	// Generate DEK encrypted with master key.
	dekEnc, err := km.GenerateDEK(ctx)
	if err != nil {
		return fmt.Errorf("generate DEK: %w", err)
	}

	// Generate Arx signing keypair encrypted with DEK.
	arxPub, arxPriv, arxPrivEnc, err := km.GenerateSigningKeypairExport(ctx, dekEnc)
	if err != nil {
		return fmt.Errorf("generate signing keypair: %w", err)
	}

	// Connect to database.
	pool, err := database.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	// Insert test tenant.
	var tenantID string
	err = pool.QueryRow(ctx, `
		INSERT INTO tenants (
			name, merchant_public_key,
			arx_signing_public_key, arx_signing_private_key_enc,
			dek_enc, session_max_expiry, max_refreshes
		) VALUES ($1, $2, $3, $4, $5, '1 hour', 5)
		ON CONFLICT (name) DO UPDATE SET
			merchant_public_key = EXCLUDED.merchant_public_key,
			arx_signing_public_key = EXCLUDED.arx_signing_public_key,
			arx_signing_private_key_enc = EXCLUDED.arx_signing_private_key_enc,
			dek_enc = EXCLUDED.dek_enc
		RETURNING id::text
	`, "bruno-test-tenant", []byte(merchantPub), []byte(arxPub), arxPrivEnc, dekEnc).Scan(&tenantID)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}

	// Also insert a pending OAuth flow for Flow 1 tests.
	_, err = pool.Exec(ctx, `
		INSERT INTO oauth_pending_flows (uuid, tenant_id, client_id, redirect_uri, state, code_challenge, expires_at)
		VALUES ('test-oauth-uuid', $1::uuid, 'test-client', 'https://example.com/callback', 'test-state', 'test-challenge', now() + interval '10 minutes')
		ON CONFLICT (uuid) DO NOTHING
	`, tenantID)
	if err != nil {
		return fmt.Errorf("insert oauth flow: %w", err)
	}

	// Output values for Bruno .env file.
	merchantSeed := merchantPriv.Seed()
	arxSeed := arxPriv.Seed()
	fmt.Println("# Paste into e2e/bruno/arx-webhooks/.env")
	fmt.Printf("ARX_MASTER_KEY=%s\n", masterKeyHex)
	fmt.Printf("BASE_URL=http://localhost:8080\n")
	fmt.Printf("TENANT_ID=%s\n", tenantID)
	fmt.Printf("MERCHANT_PRIVATE_KEY=%s\n", hex.EncodeToString(merchantSeed))
	fmt.Printf("MERCHANT_PUBLIC_KEY=%s\n", hex.EncodeToString(merchantPub))
	fmt.Printf("ARX_SIGNING_PRIVATE_KEY=%s\n", hex.EncodeToString(arxSeed))

	fmt.Fprintf(os.Stderr, "\nTest tenant seeded: %s\n", tenantID)
	return nil
}
