package scope

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrConstraintViolation indicates a tool call parameter violated a scope
// constraint.
var ErrConstraintViolation = errors.New("constraint_violation")

// ErrConstraintFormatMismatch indicates a tool call parameter has the wrong
// type for the constraint comparison.
var ErrConstraintFormatMismatch = errors.New("constraint_format_mismatch")

// ConstraintViolationError carries details about which constraint was violated.
type ConstraintViolationError struct {
	// Constraint is the constraint key (e.g., "maxAmount").
	Constraint string
	// Limit is the constraint value from the scope.
	Limit string
	// Actual is the value provided in the tool call.
	Actual string
}

func (e *ConstraintViolationError) Error() string {
	return fmt.Sprintf("constraint_violation: %s limit=%s actual=%s", e.Constraint, e.Limit, e.Actual)
}

func (e *ConstraintViolationError) Unwrap() error {
	return ErrConstraintViolation
}

// constraintParamName maps well-known constraint keys to the tool call
// parameter name they constrain. For example, "maxAmount" constrains "amount".
var constraintParamName = map[string]string{
	"maxAmount": "amount",
	"minAmount": "amount",
	"maxPrice":  "max_price",
	"maxQty":    "quantity",
}

// paramNameForConstraint returns the tool call parameter name that corresponds
// to the given constraint key. If no mapping exists, the constraint key itself
// is used (e.g., "currency" → "currency").
func paramNameForConstraint(key string) string {
	if mapped, ok := constraintParamName[key]; ok {
		return mapped
	}
	return key
}

// EvaluateConstraints checks each constraint against the tool call params.
// Numeric constraints (keys starting with "max" or "min") compare the param
// value numerically. String constraints with pipe-separated values are treated
// as enums. Other string constraints require an exact match.
func EvaluateConstraints(constraints []Constraint, params map[string]any) error {
	for _, c := range constraints {
		paramName := paramNameForConstraint(c.Key)
		paramVal, ok := params[paramName]
		if !ok {
			// Parameter not present — nothing to constrain.
			continue
		}

		if isNumericConstraint(c.Key) {
			if err := evaluateNumeric(c, paramVal); err != nil {
				return err
			}
			continue
		}

		if err := evaluateString(c, paramVal); err != nil {
			return err
		}
	}
	return nil
}

// isNumericConstraint returns true if the constraint key implies a numeric
// comparison (starts with "max" or "min").
func isNumericConstraint(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasPrefix(lower, "max") || strings.HasPrefix(lower, "min")
}

// evaluateNumeric compares a numeric constraint against a parameter value.
func evaluateNumeric(c Constraint, paramVal any) error {
	limit, err := strconv.ParseFloat(c.Value, 64)
	if err != nil {
		return fmt.Errorf("%w: constraint %q has non-numeric value %q", ErrConstraintFormatMismatch, c.Key, c.Value)
	}

	actual, ok := toFloat64(paramVal)
	if !ok {
		return fmt.Errorf("%w: parameter for constraint %q must be numeric, got %T", ErrConstraintFormatMismatch, c.Key, paramVal)
	}

	lower := strings.ToLower(c.Key)
	if strings.HasPrefix(lower, "max") && actual > limit {
		return &ConstraintViolationError{
			Constraint: c.Key,
			Limit:      c.Value,
			Actual:     strconv.FormatFloat(actual, 'f', -1, 64),
		}
	}
	if strings.HasPrefix(lower, "min") && actual < limit {
		return &ConstraintViolationError{
			Constraint: c.Key,
			Limit:      c.Value,
			Actual:     strconv.FormatFloat(actual, 'f', -1, 64),
		}
	}

	return nil
}

// evaluateString compares a string constraint against a parameter value.
// If the constraint value contains "|", it is treated as an enum (pipe-separated
// allowed values).
func evaluateString(c Constraint, paramVal any) error {
	actual, ok := paramVal.(string)
	if !ok {
		return fmt.Errorf("%w: parameter for constraint %q must be a string, got %T", ErrConstraintFormatMismatch, c.Key, paramVal)
	}

	if strings.Contains(c.Value, "|") {
		// Enum check.
		allowed := strings.Split(c.Value, "|")
		for _, v := range allowed {
			if actual == v {
				return nil
			}
		}
		return &ConstraintViolationError{
			Constraint: c.Key,
			Limit:      c.Value,
			Actual:     actual,
		}
	}

	// Exact match.
	if actual != c.Value {
		return &ConstraintViolationError{
			Constraint: c.Key,
			Limit:      c.Value,
			Actual:     actual,
		}
	}
	return nil
}

// toFloat64 attempts to convert a value to float64. It handles float64, int,
// int64, and float32 types commonly seen in JSON-decoded data.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}
