package validate

import (
	"math"
	"testing"
)

func TestAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"valid address", "pokt1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0", false},
		{"valid all numeric after prefix", "pokt100000000000000000000000000000000000000", false},
		{"valid all alpha after prefix", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"empty string", "", true},
		{"missing prefix", "a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2", true},
		{"wrong prefix", "cosmos1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0", true},
		{"too short", "pokt1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a", true},
		{"too long", "pokt1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", true},
		{"uppercase chars", "pokt1A2B3C4D5E6F7A8B9C0D1E2F3A4B5C6D7E8F9A0", true},
		{"special chars", "pokt1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9!", true},
		{"spaces", "pokt1 a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9", true},
		{"newline injection", "pokt1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8\na", true},
		{"path traversal attempt", "pokt1../../etc/passwd000000000000000000000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Address(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Address(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			}
		})
	}
}

func TestServiceID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"simple alpha", "anvil", false},
		{"alphanumeric", "svc01", false},
		{"with dashes", "my-service", false},
		{"with underscores", "my_service", false},
		{"mixed", "My_Service-01", false},
		{"single char", "a", false},
		{"max length 64", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"empty string", "", true},
		{"too long 65 chars", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"spaces", "my service", true},
		{"yaml injection colon", "svc: malicious", true},
		{"yaml injection newline", "svc\n  - injected", true},
		{"special chars", "svc@#$", true},
		{"dot", "svc.id", true},
		{"slash", "svc/id", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ServiceID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestPOKTAmount(t *testing.T) {
	tests := []struct {
		name      string
		pokt      float64
		wantUpokt int64
		wantErr   bool
	}{
		{"1 POKT", 1.0, 1_000_000, false},
		{"fractional", 0.5, 500_000, false},
		{"small amount", 0.000001, 1, false},
		{"large valid", 1000000.0, 1_000_000_000_000, false},
		{"zero", 0, 0, true},
		{"negative", -1.0, 0, true},
		{"NaN", math.NaN(), 0, true},
		{"positive infinity", math.Inf(1), 0, true},
		{"negative infinity", math.Inf(-1), 0, true},
		{"overflow", float64(math.MaxInt64), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := POKTAmount(tt.pokt)
			if (err != nil) != tt.wantErr {
				t.Errorf("POKTAmount(%v) error = %v, wantErr %v", tt.pokt, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantUpokt {
				t.Errorf("POKTAmount(%v) = %d, want %d", tt.pokt, got, tt.wantUpokt)
			}
		})
	}
}

func TestStakeAddition(t *testing.T) {
	tests := []struct {
		name    string
		current int64
		delta   int64
		want    int64
		wantErr bool
	}{
		{"normal addition", 1000, 500, 1500, false},
		{"zero current", 0, 100, 100, false},
		{"large values", 5_000_000_000, 1_000_000_000, 6_000_000_000, false},
		{"zero delta", 1000, 0, 0, true},
		{"negative delta", 1000, -1, 0, true},
		{"overflow", math.MaxInt64, 1, 0, true},
		{"near overflow", math.MaxInt64 - 1, 2, 0, true},
		{"exact max", math.MaxInt64 - 1, 1, math.MaxInt64, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := StakeAddition(tt.current, tt.delta)
			if (err != nil) != tt.wantErr {
				t.Errorf("StakeAddition(%d, %d) error = %v, wantErr %v", tt.current, tt.delta, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("StakeAddition(%d, %d) = %d, want %d", tt.current, tt.delta, got, tt.want)
			}
		})
	}
}

func TestKeyringBackend(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		wantErr bool
	}{
		{"test", "test", false},
		{"file", "file", false},
		{"os", "os", false},
		{"kwallet", "kwallet", false},
		{"pass", "pass", false},
		{"empty", "", true},
		{"invalid", "memory", true},
		{"uppercase", "TEST", true},
		{"injection", "test; rm -rf /", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := KeyringBackend(tt.backend)
			if (err != nil) != tt.wantErr {
				t.Errorf("KeyringBackend(%q) error = %v, wantErr %v", tt.backend, err, tt.wantErr)
			}
		})
	}
}

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https", "https://api.example.com", false},
		{"http", "http://localhost:26657", false},
		{"https with path", "https://api.example.com/v1", false},
		{"https with port", "https://api.example.com:443", false},
		{"sauron rpc", "https://sauron-rpc.infra.pocket.network/", false},
		{"sauron api", "https://sauron-api.infra.pocket.network/", false},
		{"empty", "", true},
		{"no scheme", "api.example.com", true},
		{"ftp scheme", "ftp://api.example.com", true},
		{"file scheme", "file:///etc/passwd", true},
		{"javascript", "javascript:alert(1)", true},
		{"no host", "http://", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Endpoint(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("Endpoint(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}
