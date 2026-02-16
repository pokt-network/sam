package autotopup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pokt-network/sam/internal/models"
)

// StoreData maps network -> address -> config.
type StoreData map[string]map[string]models.AutoTopUpConfig

// Store provides thread-safe persistence for auto-top-up configs.
type Store struct {
	mu   sync.RWMutex
	path string
	data StoreData
}

// NewStore loads or creates an auto-top-up config file.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: make(StoreData),
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("failed to create autotopup file: %w", err)
		}
		return s, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read autotopup file: %w", err)
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("failed to parse autotopup file: %w", err)
		}
	}

	return s, nil
}

// Get returns the config for a specific app on a network.
func (s *Store) Get(network, address string) (models.AutoTopUpConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if net, ok := s.data[network]; ok {
		cfg, ok := net[address]
		return cfg, ok
	}
	return models.AutoTopUpConfig{}, false
}

// GetAll returns all configs for a network.
func (s *Store) GetAll(network string) map[string]models.AutoTopUpConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]models.AutoTopUpConfig)
	if net, ok := s.data[network]; ok {
		for k, v := range net {
			result[k] = v
		}
	}
	return result
}

// GetEnabled returns all enabled configs across all networks.
func (s *Store) GetEnabled() map[string]map[string]models.AutoTopUpConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]map[string]models.AutoTopUpConfig)
	for network, apps := range s.data {
		for addr, cfg := range apps {
			if cfg.Enabled {
				if result[network] == nil {
					result[network] = make(map[string]models.AutoTopUpConfig)
				}
				result[network][addr] = cfg
			}
		}
	}
	return result
}

// Set stores or updates a config and persists to disk.
func (s *Store) Set(network, address string, cfg models.AutoTopUpConfig) error {
	if cfg.TriggerThreshold <= 0 || cfg.TargetAmount <= 0 {
		return fmt.Errorf("trigger threshold and target amount must be positive")
	}
	if cfg.TargetAmount <= cfg.TriggerThreshold {
		return fmt.Errorf("target amount must be greater than trigger threshold")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[network] == nil {
		s.data[network] = make(map[string]models.AutoTopUpConfig)
	}
	s.data[network][address] = cfg
	return s.save()
}

// Delete removes a config and persists to disk.
func (s *Store) Delete(network, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if net, ok := s.data[network]; ok {
		delete(net, address)
		if len(net) == 0 {
			delete(s.data, network)
		}
	}
	return s.save()
}

// save writes data atomically (temp file + rename).
func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal autotopup data: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "autotopup-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
