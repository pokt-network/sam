package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestConfig() *Config {
	cfg := &Config{}
	cfg.Config.Networks = map[string]NetworkConfig{
		"pocket": {
			RPCEndpoint: "https://rpc.example.com",
			APIEndpoint: "https://api.example.com",
			Bank:        "pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Applications: []string{
				"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}
	return cfg
}

func TestAddApplicationAddress(t *testing.T) {
	tests := []struct {
		name    string
		network string
		address string
		wantErr bool
	}{
		{
			name:    "add new address",
			network: "pocket",
			address: "pokt1cccccccccccccccccccccccccccccccccccccc",
			wantErr: false,
		},
		{
			name:    "duplicate address",
			network: "pocket",
			address: "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "unknown network",
			network: "nonexistent",
			address: "pokt1cccccccccccccccccccccccccccccccccccccc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeTestConfig()
			err := cfg.AddApplicationAddress(tt.network, tt.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddApplicationAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddApplicationAddress_AppendsToList(t *testing.T) {
	cfg := makeTestConfig()
	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"

	if err := cfg.AddApplicationAddress("pocket", newAddr); err != nil {
		t.Fatal(err)
	}

	net := cfg.Config.Networks["pocket"]
	if len(net.Applications) != 2 {
		t.Fatalf("expected 2 applications, got %d", len(net.Applications))
	}
	if net.Applications[1] != newAddr {
		t.Errorf("new address = %s, want %s", net.Applications[1], newAddr)
	}
}

func TestSaveApplicationAddress(t *testing.T) {
	configContent := `config:
  keyring-backend: test
  thresholds:
    warning_threshold: 2000000000
    danger_threshold: 1000000000
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      bank: pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        - pokt1dddddddddddddddddddddddddddddddddddddd
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"
	if err := SaveApplicationAddress(path, "pocket", newAddr); err != nil {
		t.Fatalf("SaveApplicationAddress() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "- "+newAddr) {
		t.Error("new address not found in file")
	}

	// Verify the new address comes after the last existing one
	lastExistingIdx := strings.LastIndex(content, "- pokt1dddddddddddddddddddddddddddddddddddddd")
	newIdx := strings.Index(content, "- "+newAddr)
	if newIdx <= lastExistingIdx {
		t.Error("new address should be inserted after last existing entry")
	}
}

func TestSaveApplicationAddress_PreservesComments(t *testing.T) {
	configContent := `config:
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
#        - pokt1commented00000000000000000000000000000
      bank: pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"
	if err := SaveApplicationAddress(path, "pocket", newAddr); err != nil {
		t.Fatalf("SaveApplicationAddress() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Comment should still be present
	if !strings.Contains(content, "#        - pokt1commented") {
		t.Error("comment was not preserved")
	}

	// New address should be present
	if !strings.Contains(content, "- "+newAddr) {
		t.Error("new address not found in file")
	}

	// Bank line should still be present
	if !strings.Contains(content, "bank: pokt1") {
		t.Error("bank line was lost")
	}
}

func TestSaveApplicationAddress_PreservesIndentation(t *testing.T) {
	configContent := `config:
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"
	SaveApplicationAddress(path, "pocket", newAddr)

	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")

	// Find the lines containing addresses and check indentation matches
	var indents []string
	for _, line := range lines {
		if strings.Contains(line, "- pokt1") {
			indent := ""
			for _, ch := range line {
				if ch == ' ' {
					indent += " "
				} else {
					break
				}
			}
			indents = append(indents, indent)
		}
	}

	if len(indents) < 2 {
		t.Fatalf("expected at least 2 address lines, got %d", len(indents))
	}

	for i := 1; i < len(indents); i++ {
		if indents[i] != indents[0] {
			t.Errorf("indentation mismatch: line 0 = %q, line %d = %q", indents[0], i, indents[i])
		}
	}
}

func TestSaveApplicationAddress_NetworkNotFound(t *testing.T) {
	configContent := `config:
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(configContent), 0600)

	err := SaveApplicationAddress(path, "nonexistent", "pokt1cccccccccccccccccccccccccccccccccccccc")
	if err == nil {
		t.Fatal("expected error for nonexistent network")
	}
}

func TestSaveApplicationAddress_MultipleNetworks(t *testing.T) {
	configContent := `config:
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    testnet:
      rpc_endpoint: https://rpc-testnet.example.com
      api_endpoint: https://api-testnet.example.com
      applications:
        - pokt1dddddddddddddddddddddddddddddddddddddd
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(configContent), 0600)

	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"
	if err := SaveApplicationAddress(path, "testnet", newAddr); err != nil {
		t.Fatalf("SaveApplicationAddress() error = %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	// New address should be in the file
	if !strings.Contains(content, "- "+newAddr) {
		t.Error("new address not found")
	}

	// Both networks should still have their original apps
	if !strings.Contains(content, "- pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Error("pocket app was lost")
	}
	if !strings.Contains(content, "- pokt1dddddddddddddddddddddddddddddddddddddd") {
		t.Error("testnet app was lost")
	}

	// New address should appear after testnet's app, not pocket's
	testnetAppIdx := strings.Index(content, "- pokt1dddddddddddddddddddddddddddddddddddddd")
	newAddrIdx := strings.Index(content, "- "+newAddr)
	pocketAppIdx := strings.Index(content, "- pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	if newAddrIdx < testnetAppIdx {
		t.Error("new address should be after testnet app")
	}
	if newAddrIdx < pocketAppIdx {
		// This is ok, but let's make sure it's placed next to testnet, not pocket
		// The new address should be closer to testnet's app
		if newAddrIdx-pocketAppIdx < testnetAppIdx-pocketAppIdx {
			t.Error("new address seems placed in pocket section, not testnet")
		}
	}
}

func TestSaveApplicationAddress_EmptyApplicationsList(t *testing.T) {
	configContent := `config:
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      applications:
      bank: pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	newAddr := "pokt1cccccccccccccccccccccccccccccccccccccc"
	if err := SaveApplicationAddress(path, "pocket", newAddr); err != nil {
		t.Fatalf("SaveApplicationAddress() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "- "+newAddr) {
		t.Error("new address not found in file")
	}

	// Bank line should still be present
	if !strings.Contains(content, "bank: pokt1") {
		t.Error("bank line was lost")
	}

	// New address should come after "applications:" and before "bank:"
	appsIdx := strings.Index(content, "applications:")
	newIdx := strings.Index(content, "- "+newAddr)
	bankIdx := strings.Index(content, "bank:")
	if newIdx <= appsIdx {
		t.Error("new address should be after applications: line")
	}
	if newIdx >= bankIdx {
		t.Error("new address should be before bank: line")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	configContent := `config:
  keyring-backend: test
  thresholds:
    warning_threshold: 2000000000
    danger_threshold: 1000000000
  networks:
    pocket:
      rpc_endpoint: https://rpc.example.com
      api_endpoint: https://api.example.com
      bank: pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      applications:
        - pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(configContent), 0600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Config.Networks) != 1 {
		t.Errorf("expected 1 network, got %d", len(cfg.Config.Networks))
	}
	if cfg.Config.Thresholds.WarningThreshold != 2000000000 {
		t.Errorf("warning_threshold = %d, want 2000000000", cfg.Config.Thresholds.WarningThreshold)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_NoNetworks(t *testing.T) {
	configContent := `config:
  thresholds:
    warning_threshold: 100
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(configContent), 0600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for no networks")
	}
}
