package crypto

import (
	"testing"
)

func TestCanonicalMessage(t *testing.T) {
	sp := SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-connected",
		Body:      []byte(`{"session_id":"abc"}`),
		Timestamp: "1700000000",
		Nonce:     "unique-nonce-1",
	}

	got := string(sp.CanonicalMessage())
	want := "POST\n/webhook/session-connected\n1700000000\nunique-nonce-1\n{\"session_id\":\"abc\"}"
	if got != want {
		t.Errorf("CanonicalMessage()\ngot:  %q\nwant: %q", got, want)
	}
}

func TestCanonicalMessageEmptyBody(t *testing.T) {
	sp := SignedPayload{
		Method:    "GET",
		Path:      "/health",
		Timestamp: "1700000000",
		Nonce:     "nonce-2",
	}

	got := string(sp.CanonicalMessage())
	want := "GET\n/health\n1700000000\nnonce-2\n"
	if got != want {
		t.Errorf("CanonicalMessage()\ngot:  %q\nwant: %q", got, want)
	}
}
