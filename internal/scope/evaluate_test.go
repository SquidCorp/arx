package scope_test

import (
	"errors"
	"testing"

	"github.com/fambr/arx/internal/scope"
)

func TestEvaluateConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		constraints []scope.Constraint
		params      map[string]any
		wantErr     error
	}{
		{
			name:        "no constraints passes",
			constraints: nil,
			params:      map[string]any{"amount": 150},
			wantErr:     nil,
		},
		{
			name: "numeric within limit passes",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"amount": float64(80)},
			wantErr: nil,
		},
		{
			name: "numeric at exact limit passes",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"amount": float64(100)},
			wantErr: nil,
		},
		{
			name: "numeric exceeds limit fails",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"amount": float64(150)},
			wantErr: scope.ErrConstraintViolation,
		},
		{
			name: "string exact match passes",
			constraints: []scope.Constraint{
				{Key: "currency", Value: "EUR"},
			},
			params:  map[string]any{"currency": "EUR"},
			wantErr: nil,
		},
		{
			name: "string mismatch fails",
			constraints: []scope.Constraint{
				{Key: "currency", Value: "EUR"},
			},
			params:  map[string]any{"currency": "USD"},
			wantErr: scope.ErrConstraintViolation,
		},
		{
			name: "multiple constraints all pass",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
				{Key: "currency", Value: "EUR"},
			},
			params:  map[string]any{"amount": float64(80), "currency": "EUR"},
			wantErr: nil,
		},
		{
			name: "multiple constraints one fails",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
				{Key: "currency", Value: "EUR"},
			},
			params:  map[string]any{"amount": float64(80), "currency": "USD"},
			wantErr: scope.ErrConstraintViolation,
		},
		{
			name: "numeric constraint with non-numeric param value",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"amount": "not-a-number"},
			wantErr: scope.ErrConstraintFormatMismatch,
		},
		{
			name: "param not in map skips constraint",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"other": float64(50)},
			wantErr: nil,
		},
		{
			name: "integer param value works",
			constraints: []scope.Constraint{
				{Key: "maxAmount", Value: "100"},
			},
			params:  map[string]any{"amount": 80},
			wantErr: nil,
		},
		{
			name: "enum constraint with pipe-separated values passes",
			constraints: []scope.Constraint{
				{Key: "currency", Value: "EUR|USD|GBP"},
			},
			params:  map[string]any{"currency": "USD"},
			wantErr: nil,
		},
		{
			name: "enum constraint no match fails",
			constraints: []scope.Constraint{
				{Key: "currency", Value: "EUR|USD|GBP"},
			},
			params:  map[string]any{"currency": "JPY"},
			wantErr: scope.ErrConstraintViolation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := scope.EvaluateConstraints(tt.constraints, tt.params)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestConstraintViolationDetails(t *testing.T) {
	t.Parallel()

	constraints := []scope.Constraint{
		{Key: "maxAmount", Value: "100"},
	}
	params := map[string]any{"amount": float64(150)}

	err := scope.EvaluateConstraints(constraints, params)
	if err == nil {
		t.Fatal("expected error")
	}

	var violation *scope.ConstraintViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("expected *ConstraintViolationError, got %T", err)
	}

	if violation.Constraint != "maxAmount" {
		t.Errorf("constraint = %q, want %q", violation.Constraint, "maxAmount")
	}
	if violation.Limit != "100" {
		t.Errorf("limit = %q, want %q", violation.Limit, "100")
	}

	// Verify Error() output format.
	errMsg := violation.Error()
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
	if !errors.Is(violation, scope.ErrConstraintViolation) {
		t.Error("expected Unwrap to return ErrConstraintViolation")
	}
}
