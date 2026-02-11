package validate

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
)

var (
	addressRe   = regexp.MustCompile(`^pokt1[a-z0-9]{38}$`)
	serviceIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

	allowedKeyringBackends = map[string]bool{
		"test":    true,
		"file":    true,
		"os":      true,
		"kwallet": true,
		"pass":    true,
	}
)

// Address validates a bech32 Pocket Network address.
func Address(addr string) error {
	if !addressRe.MatchString(addr) {
		return errors.New("invalid address format: must match pokt1 followed by 38 lowercase alphanumeric characters")
	}
	return nil
}

// ServiceID validates that a service ID is safe for YAML interpolation.
func ServiceID(id string) error {
	if !serviceIDRe.MatchString(id) {
		return errors.New("invalid service ID: must be 1-64 alphanumeric, dash, or underscore characters")
	}
	return nil
}

// POKTAmount validates a POKT amount and converts it to uPOKT.
// Rejects negative, zero, NaN, Inf, and values that would overflow int64 in uPOKT.
func POKTAmount(pokt float64) (int64, error) {
	if math.IsNaN(pokt) || math.IsInf(pokt, 0) {
		return 0, errors.New("amount must be a finite number")
	}
	if pokt <= 0 {
		return 0, errors.New("amount must be positive")
	}
	upokt := pokt * 1_000_000
	if upokt > float64(math.MaxInt64) {
		return 0, errors.New("amount too large")
	}
	return int64(upokt), nil
}

// StakeAddition checks that adding delta to current does not overflow int64.
func StakeAddition(current, delta int64) (int64, error) {
	if delta <= 0 {
		return 0, errors.New("stake addition must be positive")
	}
	sum := current + delta
	if sum < current {
		return 0, fmt.Errorf("stake overflow: %d + %d exceeds maximum", current, delta)
	}
	return sum, nil
}

// KeyringBackend validates that the backend is in the allowed set.
func KeyringBackend(backend string) error {
	if !allowedKeyringBackends[backend] {
		return fmt.Errorf("unsupported keyring backend %q: must be one of test, file, os, kwallet, pass", backend)
	}
	return nil
}

// Endpoint validates that a raw URL is a valid http or https URL with a host.
func Endpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint must use http or https scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("endpoint must have a host")
	}
	return nil
}
