package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/pokt-network/sam/internal/validate"
)

var configMu sync.Mutex

// NetworkConfig holds per-network connection and address settings.
type NetworkConfig struct {
	RPCEndpoint  string   `yaml:"rpc_endpoint"`
	APIEndpoint  string   `yaml:"api_endpoint"`
	Gateways     []string `yaml:"gateways"`
	Bank         string   `yaml:"bank"`
	Applications []string `yaml:"applications"`
}

// Config is the top-level configuration loaded from config.yaml.
type Config struct {
	Config struct {
		KeyringBackend string                   `yaml:"keyring-backend"`
		PocketdHome    string                   `yaml:"pocketd-home"`
		Thresholds     Thresholds               `yaml:"thresholds"`
		Networks       map[string]NetworkConfig `yaml:"networks"`
	} `yaml:"config"`
}

// Thresholds defines warning/danger levels in uPOKT.
type Thresholds struct {
	WarningThreshold int64 `yaml:"warning_threshold" json:"warning_threshold"`
	DangerThreshold  int64 `yaml:"danger_threshold" json:"danger_threshold"`
}

// Load reads and parses a config.yaml file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set default API endpoints if not provided.
	for name, network := range cfg.Config.Networks {
		if network.APIEndpoint == "" {
			apiEndpoint := strings.Replace(network.RPCEndpoint, ":443", "", 1)
			apiEndpoint = strings.Replace(apiEndpoint, ":26657", "", 1)
			apiEndpoint = strings.Replace(apiEndpoint, "rpc", "api", 1)
			network.APIEndpoint = apiEndpoint
			cfg.Config.Networks[name] = network
		}
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// AddApplicationAddress adds an application address to the in-memory config.
// Returns an error if the address already exists in the network.
func (c *Config) AddApplicationAddress(network, address string) error {
	configMu.Lock()
	defer configMu.Unlock()

	net, ok := c.Config.Networks[network]
	if !ok {
		return fmt.Errorf("network %q not found", network)
	}

	for _, existing := range net.Applications {
		if existing == address {
			return fmt.Errorf("address %s already exists in network %s", address, network)
		}
	}

	net.Applications = append(net.Applications, address)
	c.Config.Networks[network] = net
	return nil
}

// RemoveApplicationAddress removes an application address from the in-memory config.
// Used as a rollback when disk persistence fails after AddApplicationAddress.
func (c *Config) RemoveApplicationAddress(network, address string) {
	configMu.Lock()
	defer configMu.Unlock()

	net, ok := c.Config.Networks[network]
	if !ok {
		return
	}

	for i, existing := range net.Applications {
		if existing == address {
			net.Applications = append(net.Applications[:i], net.Applications[i+1:]...)
			c.Config.Networks[network] = net
			return
		}
	}
}

// SaveApplicationAddress inserts a new application address into config.yaml
// using targeted line insertion to preserve comments and formatting.
func SaveApplicationAddress(configPath, network, address string) error {
	configMu.Lock()
	defer configMu.Unlock()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var result []string

	// State machine to find the correct insertion point
	inTargetNetwork := false
	inApplications := false
	networkIndent := -1
	insertIdx := -1
	applicationsLineIdx := -1 // index of the "applications:" line itself
	applicationsLineIndent := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Detect network header (e.g. "    pocket:" under "networks:")
		// Only check when not already inside the target network, and require
		// the line to be a bare key (ends with ":", no spaces in key name).
		if !inTargetNetwork && !strings.HasPrefix(trimmed, "#") && strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
			name := strings.TrimSuffix(trimmed, ":")
			if name == network {
				inTargetNetwork = true
				networkIndent = indent
			}
		} else if inTargetNetwork && !inApplications && trimmed != "" && !strings.HasPrefix(trimmed, "#") && indent <= networkIndent {
			// We've left the target network block (hit a sibling or parent key)
			inTargetNetwork = false
		}

		// Detect "applications:" within the target network
		if inTargetNetwork && trimmed == "applications:" {
			inApplications = true
			applicationsLineIdx = len(result)
			// Capture indentation of the applications key for empty-list fallback
			for _, ch := range line {
				if ch == ' ' || ch == '\t' {
					applicationsLineIndent += string(ch)
				} else {
					break
				}
			}
			result = append(result, line)
			continue
		}

		// While in the applications list, track the last "- pokt1..." entry
		if inApplications {
			if strings.HasPrefix(trimmed, "- pokt1") || strings.HasPrefix(trimmed, "- ") && strings.Contains(trimmed, "pokt1") {
				insertIdx = len(result)
				result = append(result, line)
				continue
			}
			// A commented line is still part of the block
			if strings.HasPrefix(trimmed, "#") {
				result = append(result, line)
				continue
			}
			// Any other line means we've left the applications block
			if trimmed != "" {
				inApplications = false
			}
		}

		result = append(result, line)
	}

	var newEntry string

	if insertIdx != -1 {
		// Determine indentation from the last existing entry
		refLine := result[insertIdx]
		indent := ""
		for _, ch := range refLine {
			if ch == ' ' || ch == '\t' {
				indent += string(ch)
			} else {
				break
			}
		}
		newEntry = indent + "- " + address
	} else if applicationsLineIdx != -1 {
		// Empty applications list — insert after the "applications:" line.
		// Use the applications key indent + 2 extra spaces for list entries.
		insertIdx = applicationsLineIdx
		newEntry = applicationsLineIndent + "  - " + address
	} else {
		return fmt.Errorf("could not find applications section for network %q", network)
	}

	// Insert after insertIdx
	updated := make([]string, 0, len(result)+1)
	updated = append(updated, result[:insertIdx+1]...)
	updated = append(updated, newEntry)
	updated = append(updated, result[insertIdx+1:]...)

	return os.WriteFile(configPath, []byte(strings.Join(updated, "\n")), 0600)
}

func validateConfig(cfg *Config) error {
	if len(cfg.Config.Networks) == 0 {
		return fmt.Errorf("at least one network must be configured")
	}

	if cfg.Config.KeyringBackend != "" {
		if err := validate.KeyringBackend(cfg.Config.KeyringBackend); err != nil {
			return fmt.Errorf("keyring-backend: %w", err)
		}
	}

	for name, network := range cfg.Config.Networks {
		if err := validate.Endpoint(network.RPCEndpoint); err != nil {
			return fmt.Errorf("network %q rpc_endpoint: %w", name, err)
		}
		if err := validate.Endpoint(network.APIEndpoint); err != nil {
			return fmt.Errorf("network %q api_endpoint: %w", name, err)
		}
		if network.Bank != "" {
			if err := validate.Address(network.Bank); err != nil {
				return fmt.Errorf("network %q bank address: %w", name, err)
			}
		}
		for i, addr := range network.Applications {
			if err := validate.Address(addr); err != nil {
				return fmt.Errorf("network %q application[%d]: %w", name, i, err)
			}
		}
		for i, addr := range network.Gateways {
			if err := validate.Address(addr); err != nil {
				return fmt.Errorf("network %q gateway[%d]: %w", name, i, err)
			}
		}
	}

	return nil
}
