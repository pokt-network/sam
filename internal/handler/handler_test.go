package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/pokt-network/sam/internal/autotopup"
	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := &config.Config{}
	cfg.Config.Networks = map[string]config.NetworkConfig{
		"pocket": {
			RPCEndpoint: "https://rpc.example.com",
			APIEndpoint: "https://api.example.com",
			Bank:        "pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Applications: []string{
				"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}
	cfg.Config.Thresholds = config.Thresholds{
		WarningThreshold: 2000000000,
		DangerThreshold:  1000000000,
	}

	storePath := filepath.Join(t.TempDir(), "autotopup.json")
	store, err := autotopup.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	client := pocket.NewClient(logger)
	executor := pocket.NewExecutor(cfg, client, logger)
	appCache := cache.New[[]models.Application](1 * time.Minute)
	bankCache := cache.New[models.BankAccount](1 * time.Minute)

	worker := autotopup.NewWorker(store, cfg, client, executor, appCache, bankCache, logger)

	return &Server{
		Config:    cfg,
		Client:    client,
		Executor:  executor,
		AppCache:  appCache,
		BankCache: bankCache,
		AutoTopUp: store,
		Worker:    worker,
		Logger:    logger,
	}
}

func setupRouter(srv *Server) *mux.Router {
	r := mux.NewRouter()
	srv.SetupRoutes(r)
	return r
}

func TestHandleStakeNewApplication_InvalidAddress(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	body := `{"address":"invalid","service_id":"anvil","amount":100}`
	req := httptest.NewRequest("POST", "/api/applications/stake?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp models.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "invalid address format" {
		t.Errorf("error = %q, want 'invalid address format'", resp.Error)
	}
}

func TestHandleStakeNewApplication_InvalidServiceID(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	body := `{"address":"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","service_id":"bad service!","amount":100}`
	req := httptest.NewRequest("POST", "/api/applications/stake?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStakeNewApplication_InvalidAmount(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	body := `{"address":"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","service_id":"anvil","amount":-5}`
	req := httptest.NewRequest("POST", "/api/applications/stake?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStakeNewApplication_InvalidNetwork(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	body := `{"address":"pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","service_id":"anvil","amount":100}`
	req := httptest.NewRequest("POST", "/api/applications/stake?network=nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStakeNewApplication_InvalidBody(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("POST", "/api/applications/stake?network=pocket", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetAutoTopUp_Valid(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := `{"enabled":true,"trigger_threshold":1000,"target_amount":5000}`
	req := httptest.NewRequest("PUT", "/api/applications/"+addr+"/autotopup?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var cfg models.AutoTopUpConfig
	json.NewDecoder(w.Body).Decode(&cfg)
	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.TriggerThreshold != 1000000000 {
		t.Errorf("TriggerThreshold = %d, want 1000000000", cfg.TriggerThreshold)
	}
	if cfg.TargetAmount != 5000000000 {
		t.Errorf("TargetAmount = %d, want 5000000000", cfg.TargetAmount)
	}
}

func TestHandleSetAutoTopUp_InvalidAddress(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	body := `{"enabled":true,"trigger_threshold":1000,"target_amount":5000}`
	req := httptest.NewRequest("PUT", "/api/applications/badaddr/autotopup?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetAutoTopUp_TargetMustExceedTrigger(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := `{"enabled":true,"trigger_threshold":5000,"target_amount":1000}`
	req := httptest.NewRequest("PUT", "/api/applications/"+addr+"/autotopup?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp models.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "target amount must be greater than trigger threshold" {
		t.Errorf("error = %q", resp.Error)
	}
}

func TestHandleSetAutoTopUp_EqualTargetAndTrigger(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := `{"enabled":true,"trigger_threshold":1000,"target_amount":1000}`
	req := httptest.NewRequest("PUT", "/api/applications/"+addr+"/autotopup?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for equal target/trigger", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetAutoTopUp_ZeroThreshold(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := `{"enabled":true,"trigger_threshold":0,"target_amount":5000}`
	req := httptest.NewRequest("PUT", "/api/applications/"+addr+"/autotopup?network=pocket", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for zero threshold", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDeleteAutoTopUp(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// First set a config
	srv.AutoTopUp.Set("pocket", addr, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 1000, TargetAmount: 5000})

	req := httptest.NewRequest("DELETE", "/api/applications/"+addr+"/autotopup?network=pocket", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify it's deleted
	_, ok := srv.AutoTopUp.Get("pocket", addr)
	if ok {
		t.Error("config should be deleted")
	}
}

func TestHandleDeleteAutoTopUp_InvalidAddress(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("DELETE", "/api/applications/badaddr/autotopup?network=pocket", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetAutoTopUp(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	addr := "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	srv.AutoTopUp.Set("pocket", addr, models.AutoTopUpConfig{Enabled: true, TriggerThreshold: 1000, TargetAmount: 5000})

	req := httptest.NewRequest("GET", "/api/autotopup?network=pocket", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var configs map[string]models.AutoTopUpConfig
	json.NewDecoder(w.Body).Decode(&configs)
	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if _, ok := configs[addr]; !ok {
		t.Error("expected config for addr")
	}
}

func TestHandleGetAutoTopUp_Empty(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("GET", "/api/autotopup?network=pocket", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var configs map[string]models.AutoTopUpConfig
	json.NewDecoder(w.Body).Decode(&configs)
	if len(configs) != 0 {
		t.Errorf("expected empty map, got %d entries", len(configs))
	}
}

func TestHandleGetAutoTopUpEvents(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("GET", "/api/autotopup/events", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var events []models.AutoTopUpEvent
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestHandleGetServices_InvalidNetwork(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("GET", "/api/services?network=nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetNetworks(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("GET", "/api/networks", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var networks []string
	json.NewDecoder(w.Body).Decode(&networks)
	if len(networks) != 1 || networks[0] != "pocket" {
		t.Errorf("networks = %v, want [pocket]", networks)
	}
}

func TestHandleGetConfig(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if _, ok := result["thresholds"]; !ok {
		t.Error("response should contain thresholds")
	}
	// Should NOT contain network details
	if _, ok := result["networks"]; ok {
		t.Error("response should not expose networks")
	}
}

func TestDefaultNetworkFallback(t *testing.T) {
	srv := newTestServer(t)
	router := setupRouter(srv)

	// GET /api/autotopup without network param should default to "pocket"
	req := httptest.NewRequest("GET", "/api/autotopup", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
