// Package session provides session lifecycle management including state machine
// enforcement and transition validation.
package session

import "errors"

// Status represents a session's current state.
type Status string

const (
	// StatusActive indicates the session is active and usable.
	StatusActive Status = "active"
	// StatusExpired indicates the session has expired (terminal).
	StatusExpired Status = "expired"
	// StatusRevoked indicates the session was explicitly revoked (terminal).
	StatusRevoked Status = "revoked"
	// StatusSuspended indicates the session is temporarily suspended.
	StatusSuspended Status = "suspended"
)

// Transition errors returned when an invalid state transition is attempted.
var (
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrSessionExpired    = errors.New("session_expired")
	ErrSessionRevoked    = errors.New("session_revoked")
	ErrSessionSuspended  = errors.New("session_suspended")
	ErrSessionNotActive  = errors.New("session_not_active")
	ErrSessionNotSusp    = errors.New("session_not_suspended")
)

// validTransitions defines the allowed state transitions.
// Terminal states (expired, revoked) have no outgoing transitions.
var validTransitions = map[Status]map[Status]bool{
	StatusActive: {
		StatusExpired:   true,
		StatusRevoked:   true,
		StatusSuspended: true,
	},
	StatusSuspended: {
		StatusActive:  true,
		StatusRevoked: true,
	},
	// StatusExpired and StatusRevoked are terminal — no transitions allowed.
}

// ValidateTransition checks whether transitioning from the current status to
// the target status is allowed by the session state machine.
func ValidateTransition(from, to Status) error {
	allowed, ok := validTransitions[from]
	if !ok {
		return ErrInvalidTransition
	}
	if !allowed[to] {
		return ErrInvalidTransition
	}
	return nil
}

// ParseStatus converts a string to a Status, returning an error if the string
// is not a recognised session status.
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusActive, StatusExpired, StatusRevoked, StatusSuspended:
		return Status(s), nil
	default:
		return "", errors.New("unknown session status: " + s)
	}
}
