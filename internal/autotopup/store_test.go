package autotopup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pokt-network/sam/internal/models"
)

func tempStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "autotopup.json")
}

func TestNewStore_CreatesFile(t *testing.T) {
	path := tempStorePath(t)

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("NewStore() did not create file")
	}

	all := s.GetAll("pocket")
	if len(all) != 0 {
		t.Errorf("new store should be empty, got %d entries", len(all))
	}
}

func TestNewStore_LoadsExisting(t *testing.T) {
	path := tempStorePath(t)

	data := StoreData{
		"pocket": {
			"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": models.AutoTopUpConfig{
				Enabled:          true,
				TriggerThreshold: 1000000000,
				TargetAmount:     5000000000,
			},
		},
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	cfg, ok := s.Get("pocket", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if !ok {
		t.Fatal("expected to find config after loading")
	}
	if cfg.TriggerThreshold != 1000000000 {
		t.Errorf("TriggerThreshold = %d, want 1000000000", cfg.TriggerThreshold)
	}
	if cfg.TargetAmount != 5000000000 {
		t.Errorf("TargetAmount = %d, want 5000000000", cfg.TargetAmount)
	}
}

func TestNewStore_InvalidJSON(t *testing.T) {
	path := tempStorePath(t)
	if err := os.WriteFile(path, []byte("{bad json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := NewStore(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestStore_SetAndGet(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 1000,
		TargetAmount:     5000,
	}

	if err := s.Set("pocket", addr, cfg); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, ok := s.Get("pocket", addr)
	if !ok {
		t.Fatal("Get() returned false after Set()")
	}
	if got != cfg {
		t.Errorf("Get() = %+v, want %+v", got, cfg)
	}
}

func TestStore_GetMissing(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	_, ok := s.Get("pocket", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if ok {
		t.Error("Get() on empty store should return false")
	}

	_, ok = s.Get("nonexistent", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if ok {
		t.Error("Get() on nonexistent network should return false")
	}
}

func TestStore_GetAll(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr1 := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addr2 := "pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	s.Set("pocket", addr1, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 100, TargetAmount: 500})
	s.Set("pocket", addr2, models.AutoTopUpConfig{Enabled: false, TriggerThreshold: 200, TargetAmount: 600})
	s.Set("testnet", addr1, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 300, TargetAmount: 700})

	all := s.GetAll("pocket")
	if len(all) != 2 {
		t.Errorf("GetAll(pocket) returned %d entries, want 2", len(all))
	}

	all = s.GetAll("testnet")
	if len(all) != 1 {
		t.Errorf("GetAll(testnet) returned %d entries, want 1", len(all))
	}

	all = s.GetAll("nonexistent")
	if len(all) != 0 {
		t.Errorf("GetAll(nonexistent) returned %d entries, want 0", len(all))
	}
}

func TestStore_GetEnabled(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr1 := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addr2 := "pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	s.Set("pocket", addr1, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 100, TargetAmount: 500})
	s.Set("pocket", addr2, models.AutoTopUpConfig{Enabled: false, TriggerThreshold: 200, TargetAmount: 600})

	enabled := s.GetEnabled()
	if len(enabled) != 1 {
		t.Fatalf("GetEnabled() returned %d networks, want 1", len(enabled))
	}
	if len(enabled["pocket"]) != 1 {
		t.Errorf("GetEnabled()[pocket] returned %d entries, want 1", len(enabled["pocket"]))
	}
	if _, ok := enabled["pocket"][addr1]; !ok {
		t.Errorf("GetEnabled() should contain addr1")
	}
}

func TestStore_Delete(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s.Set("pocket", addr, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 100, TargetAmount: 500})

	if err := s.Delete("pocket", addr); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, ok := s.Get("pocket", addr)
	if ok {
		t.Error("Get() should return false after Delete()")
	}

	// Network should be cleaned up too
	all := s.GetAll("pocket")
	if len(all) != 0 {
		t.Errorf("network should be removed when last entry deleted, got %d", len(all))
	}
}

func TestStore_DeleteNonexistent(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	// Should not error on deleting nonexistent entries
	if err := s.Delete("pocket", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("Delete() on nonexistent should not error, got %v", err)
	}
}

func TestStore_Persistence(t *testing.T) {
	path := tempStorePath(t)

	s1, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg := models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 1000, TargetAmount: 5000}
	s1.Set("pocket", addr, cfg)

	// Load a second store from the same file
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("second NewStore() error = %v", err)
	}

	got, ok := s2.Get("pocket", addr)
	if !ok {
		t.Fatal("data not persisted to disk")
	}
	if got != cfg {
		t.Errorf("persisted data = %+v, want %+v", got, cfg)
	}
}

func TestStore_Update(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s.Set("pocket", addr, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 100, TargetAmount: 500})
	s.Set("pocket", addr, models.AutoTopUpConfig{Enabled: false, TriggerThreshold: 200, TargetAmount: 600})

	got, _ := s.Get("pocket", addr)
	if got.Enabled != false {
		t.Errorf("Enabled = %v, want false after update", got.Enabled)
	}
	if got.TriggerThreshold != 200 {
		t.Errorf("TriggerThreshold = %d, want 200 after update", got.TriggerThreshold)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			cfg := models.AutoTopUpConfig{
				Enabled:          n%2 == 0,
				TriggerThreshold: int64(n * 100),
				TargetAmount:     int64(n * 500),
			}
			s.Set("pocket", addr, cfg)
			s.Get("pocket", addr)
			s.GetAll("pocket")
			s.GetEnabled()
		}(i)
	}
	wg.Wait()

	// Just verify no panics and store is readable
	_, ok := s.Get("pocket", "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if !ok {
		t.Error("expected entry to exist after concurrent writes")
	}
}

func TestStore_GetAllReturnsCopy(t *testing.T) {
	s, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s.Set("pocket", addr, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 100, TargetAmount: 500})

	all := s.GetAll("pocket")
	// Mutate the returned map
	all["pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] = models.AutoTopUpConfig{}

	// Original store should not be affected
	allAgain := s.GetAll("pocket")
	if len(allAgain) != 1 {
		t.Errorf("GetAll() should return a copy, but mutation affected store: got %d entries", len(allAgain))
	}
}
