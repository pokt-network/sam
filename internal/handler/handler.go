package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"

	"github.com/gorilla/mux"

	"github.com/pokt-network/sam/internal/autotopup"
	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
	"github.com/pokt-network/sam/internal/validate"
)

// Server holds all dependencies for HTTP handlers.
type Server struct {
	Config     *config.Config
	ConfigPath string
	Client     *pocket.Client
	Executor   *pocket.Executor
	AppCache   *cache.Cache[[]models.Application]
	BankCache  *cache.Cache[models.BankAccount]
	AutoTopUp  *autotopup.Store
	Worker     *autotopup.Worker
	Logger     *slog.Logger
}

func (s *Server) handleGetApplications(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"

	s.Logger.Info("fetching applications", "network", network, "force_refresh", forceRefresh)

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		s.Logger.Warn("invalid network requested", "network", network)
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	if !forceRefresh {
		if apps, ok := s.AppCache.Get(network); ok {
			s.Logger.Info("returning cached applications", "count", len(apps))
			respondWithJSON(w, http.StatusOK, apps)
			return
		}
	}

	s.Logger.Info("querying applications in parallel via API", "count", len(networkConfig.Applications))

	type result struct {
		app *models.Application
		err error
	}

	results := make(chan result, len(networkConfig.Applications))

	for _, appAddress := range networkConfig.Applications {
		go func(addr string) {
			defer func() {
				if r := recover(); r != nil {
					results <- result{err: fmt.Errorf("panic querying application %s: %v", addr, r)}
				}
			}()
			app, err := s.Client.QueryApplication(addr, networkConfig.APIEndpoint, network)
			results <- result{app: app, err: err}
		}(appAddress)
	}

	var applications []models.Application
	for i := 0; i < len(networkConfig.Applications); i++ {
		res := <-results
		if res.err != nil {
			s.Logger.Error("failed to query application", "error", res.err)
			continue
		}
		applications = append(applications, *res.app)
	}

	s.AppCache.Set(network, applications)

	s.Logger.Info("fetched applications", "success", len(applications), "total", len(networkConfig.Applications))
	respondWithJSON(w, http.StatusOK, applications)
}

func (s *Server) handleGetApplication(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	if err := validate.Address(address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	s.Logger.Info("fetching application", "address", address, "network", network)

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	app, err := s.Client.QueryApplication(address, networkConfig.APIEndpoint, network)
	if err != nil {
		s.Logger.Error("error querying application", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to query application")
		return
	}

	respondWithJSON(w, http.StatusOK, app)
}

func (s *Server) handleGetBank(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"

	s.Logger.Info("fetching bank account", "network", network, "force_refresh", forceRefresh)

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		s.Logger.Warn("invalid network requested", "network", network)
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	if networkConfig.Bank == "" {
		respondWithError(w, http.StatusBadRequest, "no bank account configured for network")
		return
	}

	if !forceRefresh {
		if bank, ok := s.BankCache.Get(network); ok {
			s.Logger.Info("returning cached bank account")
			respondWithJSON(w, http.StatusOK, bank)
			return
		}
	}

	bank, err := s.Client.QueryBankAccount(networkConfig.Bank, networkConfig.APIEndpoint, network)
	if err != nil {
		s.Logger.Error("error querying bank account", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to query bank account")
		return
	}

	s.BankCache.Set(network, *bank)

	respondWithJSON(w, http.StatusOK, bank)
}

func (s *Server) handleUpstake(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	if err := validate.Address(address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req models.StakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	amountUpokt, err := validate.POKTAmount(req.Amount)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger.Info("upstaking", "address", address, "pokt", req.Amount, "upokt", amountUpokt)

	result, err := s.Executor.UpstakeApplication(address, networkConfig.Bank, network, amountUpokt, networkConfig.RPCEndpoint, networkConfig.APIEndpoint)
	if err != nil {
		s.Logger.Error("upstake error", "error", err)
		respondWithError(w, http.StatusInternalServerError, "upstake operation failed")
		return
	}

	s.AppCache.Delete(network)
	s.BankCache.Delete(network)

	respondWithJSON(w, http.StatusOK, result)
}

func (s *Server) handleFund(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	if err := validate.Address(address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req models.StakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	amountUpokt, err := validate.POKTAmount(req.Amount)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger.Info("funding", "address", address, "pokt", req.Amount, "upokt", amountUpokt)

	result, err := s.Executor.FundApplication(address, networkConfig.Bank, network, amountUpokt, networkConfig.RPCEndpoint)
	if err != nil {
		s.Logger.Error("fund error", "error", err)
		respondWithError(w, http.StatusInternalServerError, "fund operation failed")
		return
	}

	s.BankCache.Delete(network)

	respondWithJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetServices(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	services, err := s.Client.QueryServices(networkConfig.APIEndpoint)
	if err != nil {
		s.Logger.Error("error querying services", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to query services")
		return
	}

	respondWithJSON(w, http.StatusOK, services)
}

func (s *Server) handleStakeNewApplication(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	networkConfig, ok := s.Config.Config.Networks[network]
	if !ok {
		respondWithError(w, http.StatusBadRequest, "invalid network")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req models.NewStakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validate.Address(req.Address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	if err := validate.ServiceID(req.ServiceID); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	amountUpokt, err := validate.POKTAmount(req.Amount)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger.Info("staking new application",
		"address", req.Address,
		"service_id", req.ServiceID,
		"pokt", req.Amount,
		"upokt", amountUpokt,
	)

	result, err := s.Executor.StakeNewApplication(req.Address, req.ServiceID, network, amountUpokt, networkConfig.RPCEndpoint)
	if err != nil {
		s.Logger.Error("stake new app error", "error", err)
		respondWithError(w, http.StatusInternalServerError, "stake operation failed")
		return
	}

	if result.Success {
		if err := s.Config.AddApplicationAddress(network, req.Address); err != nil {
			s.Logger.Warn("failed to add address to in-memory config", "error", err)
		} else if s.ConfigPath != "" {
			if err := config.SaveApplicationAddress(s.ConfigPath, network, req.Address); err != nil {
				s.Logger.Error("failed to persist address to config.yaml", "error", err)
			}
		}
	}

	s.AppCache.Delete(network)
	s.BankCache.Delete(network)

	respondWithJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetAutoTopUp(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	configs := s.AutoTopUp.GetAll(network)
	respondWithJSON(w, http.StatusOK, configs)
}

func (s *Server) handleSetAutoTopUp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	if err := validate.Address(address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var req models.AutoTopUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	triggerUpokt, err := validate.POKTAmount(req.TriggerThreshold)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("invalid trigger threshold: %s", err.Error()))
		return
	}

	targetUpokt, err := validate.POKTAmount(req.TargetAmount)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("invalid target amount: %s", err.Error()))
		return
	}

	if targetUpokt <= triggerUpokt {
		respondWithError(w, http.StatusBadRequest, "target amount must be greater than trigger threshold")
		return
	}

	cfg := models.AutoTopUpConfig{
		Enabled:          req.Enabled,
		TriggerThreshold: triggerUpokt,
		TargetAmount:     targetUpokt,
	}

	if err := s.AutoTopUp.Set(network, address, cfg); err != nil {
		s.Logger.Error("failed to save auto-top-up config", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	s.Logger.Info("auto-top-up config updated",
		"address", address, "network", network, "enabled", req.Enabled)

	respondWithJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleDeleteAutoTopUp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	if err := validate.Address(address); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid address format")
		return
	}

	network := r.URL.Query().Get("network")
	if network == "" {
		network = "pocket"
	}

	if err := s.AutoTopUp.Delete(network, address); err != nil {
		s.Logger.Error("failed to delete auto-top-up config", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to delete config")
		return
	}

	s.Logger.Info("auto-top-up config deleted", "address", address, "network", network)

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleGetAutoTopUpEvents(w http.ResponseWriter, _ *http.Request) {
	events := s.Worker.Events()
	respondWithJSON(w, http.StatusOK, events)
}

func (s *Server) handleGetNetworks(w http.ResponseWriter, _ *http.Request) {
	networks := make([]string, 0, len(s.Config.Config.Networks))
	for name := range s.Config.Config.Networks {
		networks = append(networks, name)
	}
	respondWithJSON(w, http.StatusOK, networks)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	// Only expose thresholds — network names are available via /api/networks.
	// Do NOT expose full NetworkConfig (RPC/API endpoints, bank/app addresses).
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"thresholds": s.Config.Config.Thresholds,
	})
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/index.html")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	_, err := exec.LookPath("pocketd")
	if err != nil {
		respondWithJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "unhealthy",
			"error":  "pocketd not found in PATH",
		})
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "healthy",
		"pocketd":  "available",
		"networks": len(s.Config.Config.Networks),
		"method":   "direct_api",
	})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to marshal response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, models.ErrorResponse{Error: message})
}
