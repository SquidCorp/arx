package session

import (
	"testing"
)

func TestValidateTransition_AllowedFromActive(t *testing.T) {
	allowed := []Status{StatusExpired, StatusRevoked, StatusSuspended}
	for _, to := range allowed {
		if err := ValidateTransition(StatusActive, to); err != nil {
			t.Errorf("active → %s: unexpected error: %v", to, err)
		}
	}
}

func TestValidateTransition_AllowedFromSuspended(t *testing.T) {
	allowed := []Status{StatusActive, StatusRevoked}
	for _, to := range allowed {
		if err := ValidateTransition(StatusSuspended, to); err != nil {
			t.Errorf("suspended → %s: unexpected error: %v", to, err)
		}
	}
}

func TestValidateTransition_TerminalStatesReject(t *testing.T) {
	terminals := []Status{StatusExpired, StatusRevoked}
	targets := []Status{StatusActive, StatusExpired, StatusRevoked, StatusSuspended}

	for _, from := range terminals {
		for _, to := range targets {
			err := ValidateTransition(from, to)
			if err == nil {
				t.Errorf("%s → %s: expected error for terminal state transition", from, to)
			}
		}
	}
}

func TestValidateTransition_SuspendedCannotExpire(t *testing.T) {
	if err := ValidateTransition(StatusSuspended, StatusExpired); err == nil {
		t.Error("suspended → expired: expected error")
	}
}

func TestValidateTransition_SuspendedCannotSelfTransition(t *testing.T) {
	if err := ValidateTransition(StatusSuspended, StatusSuspended); err == nil {
		t.Error("suspended → suspended: expected error")
	}
}

func TestValidateTransition_ActiveCannotSelfTransition(t *testing.T) {
	if err := ValidateTransition(StatusActive, StatusActive); err == nil {
		t.Error("active → active: expected error")
	}
}

func TestParseStatus_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  Status
	}{
		{"active", StatusActive},
		{"expired", StatusExpired},
		{"revoked", StatusRevoked},
		{"suspended", StatusSuspended},
	}
	for _, tc := range cases {
		got, err := ParseStatus(tc.input)
		if err != nil {
			t.Errorf("ParseStatus(%q): unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("ParseStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseStatus_Invalid(t *testing.T) {
	_, err := ParseStatus("unknown")
	if err == nil {
		t.Error("ParseStatus(\"unknown\"): expected error")
	}
}
