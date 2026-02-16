package handler

import (
	"net/http"

	"github.com/gorilla/mux"
)

// SetupRoutes registers all HTTP routes on the given router.
func (s *Server) SetupRoutes(r *mux.Router) {
	r.HandleFunc("/health", s.handleHealth).Methods("GET")

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/applications", s.handleGetApplications).Methods("GET")
	api.HandleFunc("/applications/stake", s.handleStakeNewApplication).Methods("POST")
	api.HandleFunc("/applications/{address}", s.handleGetApplication).Methods("GET")
	api.HandleFunc("/applications/{address}/upstake", s.handleUpstake).Methods("POST")
	api.HandleFunc("/applications/{address}/fund", s.handleFund).Methods("POST")
	api.HandleFunc("/applications/{address}/autotopup", s.handleSetAutoTopUp).Methods("PUT")
	api.HandleFunc("/applications/{address}/autotopup", s.handleDeleteAutoTopUp).Methods("DELETE")
	api.HandleFunc("/bank", s.handleGetBank).Methods("GET")
	api.HandleFunc("/networks", s.handleGetNetworks).Methods("GET")
	api.HandleFunc("/services", s.handleGetServices).Methods("GET")
	api.HandleFunc("/autotopup", s.handleGetAutoTopUp).Methods("GET")
	api.HandleFunc("/autotopup/events", s.handleGetAutoTopUpEvents).Methods("GET")
	api.HandleFunc("/config", s.handleGetConfig).Methods("GET")

	r.HandleFunc("/", s.handleFrontend).Methods("GET")
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("web")))
}
