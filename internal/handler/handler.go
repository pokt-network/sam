package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"

	"github.com/gorilla/mux"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
	"github.com/pokt-network/sam/internal/validate"
)

// Server holds all dependencies for HTTP handlers.
type Server struct {
	Config   *config.Config
	Client   *pocket.Client
	Executor *pocket.Executor
	AppCache  *cache.Cache[[]models.Application]
	BankCache *cache.Cache[models.BankAccount]
	Logger   *slog.Logger
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
