// Package main provides a utility for parsing test tokens.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/fambr/arx/internal/database"
	"github.com/fambr/arx/internal/token"
)

func main() {
	pool, err := database.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/arx?sslmode=disable")
	if err != nil {
		fmt.Println("DB error:", err)
		return
	}
	defer pool.Close()

	var pubKey []byte
	err = pool.QueryRow(context.Background(), "SELECT arx_signing_public_key FROM tenants WHERE name = 'bruno-test-tenant'").Scan(&pubKey)
	if err != nil {
		fmt.Println("Query error:", err)
		return
	}
	fmt.Println("Public key length:", len(pubKey))
	fmt.Println("Public key hex:", hex.EncodeToString(pubKey))

	// Bruno test token
	tokenStr := "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwOi8vbG9jYWxob3N0OjgwODAiLCJzdWIiOiJ0ZXN0LXNlc3Npb24iLCJhdWQiOlsiYXJ4Il0sImV4cCI6MTc3NzE1NTAwNSwiaWF0IjoxNzc3MTUxNDA1LCJjbmYiOnsiamt0IjoidGVzdC10aHVtYnByaW50In0sInNjb3BlIjoiY2FydC52aWV3IiwidGVuYW50IjoiNWQ4MmNmOTctOTlmZi00OTZlLWFjMzktYTMyZDViMWNjZWVlIn0.hbIw6O6xx8DJlgcYz6t_WXY8fUQGEu1KixWftbBmg3lr52UMR6oCQH_2PJEZRtRQFpl3qe9KpTG7Lynn1uZ-Bg"

	// Parse with Go
	claims, err := token.ParseAccessToken(tokenStr, pubKey)
	if err != nil {
		fmt.Println("Parse error:", err)
	} else {
		fmt.Printf("Claims: iss=%s sub=%s tenant=%s\n", claims.Issuer, claims.Subject, claims.Tenant)
	}

	// Now verify manually
	parts := strings.Split(tokenStr, ".")
	var sigBytes []byte
	if len(parts) == 3 {
		sigBytes, _ = base64.RawURLEncoding.DecodeString(parts[2])
		msg := parts[0] + "." + parts[1]
		valid := ed25519.Verify(pubKey, []byte(msg), sigBytes)
		fmt.Println("Manual verify Bruno token:", valid)
		fmt.Println("Signature length:", len(sigBytes))
	}

	// Create a Go token with the same seed and compare
	seed, _ := hex.DecodeString("5210ecab93ad0ca57475a1458311e17d9ea64f12cc028f76bef2fb5321fd35a1")
	goPrivKey := ed25519.NewKeyFromSeed(seed)
	goPubKey := goPrivKey.Public().(ed25519.PublicKey)
	fmt.Println("Go derived pub key hex:", hex.EncodeToString(goPubKey))
	fmt.Println("DB pub key matches:", hex.EncodeToString(goPubKey) == hex.EncodeToString(pubKey))

	msg := parts[0] + "." + parts[1]
	goSig := ed25519.Sign(goPrivKey, []byte(msg))
	goToken := msg + "." + base64.RawURLEncoding.EncodeToString(goSig)
	fmt.Println("Go token:", goToken)

	claims2, err := token.ParseAccessToken(goToken, pubKey)
	if err != nil {
		fmt.Println("Go token parse error:", err)
	} else {
		fmt.Printf("Go token claims: iss=%s sub=%s tenant=%s\n", claims2.Issuer, claims2.Subject, claims2.Tenant)
	}

	// Compare signatures
	fmt.Println("Bruno sig hex:", hex.EncodeToString(sigBytes))
	fmt.Println("Go sig hex:", hex.EncodeToString(goSig))
	fmt.Println("Sigs match:", hex.EncodeToString(sigBytes) == hex.EncodeToString(goSig))
}
