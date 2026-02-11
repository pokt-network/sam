package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pokt-network/sam/internal/validate"
)

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
