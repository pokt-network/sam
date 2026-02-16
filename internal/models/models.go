package models

import "time"

// Application represents a staked Pocket Network application.
type Application struct {
	Address       string `json:"address"`
	ServiceID     string `json:"service_id"`
	Stake         int64  `json:"stake"`
	LiquidBalance int64  `json:"liquid_balance"`
	Gateway       string `json:"gateway"`
	Network       string `json:"network"`
}

// BankAccount represents a bank account balance on a network.
type BankAccount struct {
	Address string `json:"address"`
	Balance int64  `json:"balance"`
	Network string `json:"network"`
}

// StakeRequest is the JSON body for upstake/fund POST endpoints.
type StakeRequest struct {
	Amount float64 `json:"amount"` // In POKT
}

// TransactionResponse is returned after a write transaction.
type TransactionResponse struct {
	TxHash  string `json:"tx_hash"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// API response structures for Pocket Network REST endpoints.

type APIApplicationResponse struct {
	Application struct {
		Address                   string          `json:"address"`
		Stake                     *Coin           `json:"stake"`
		ServiceConfigs            []ServiceConfig `json:"service_configs"`
		DelegateeGatewayAddresses []string        `json:"delegatee_gateway_addresses"`
	} `json:"application"`
}

type APIBalanceResponse struct {
	Balances []Coin `json:"balances"`
}

type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

type ServiceConfig struct {
	ServiceID string   `json:"service_id"`
	Service   *Service `json:"service"`
}

type Service struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NewStakeRequest is the JSON body for staking a new application.
type NewStakeRequest struct {
	Address   string  `json:"address"`
	ServiceID string  `json:"service_id"`
	Amount    float64 `json:"amount"` // In POKT
}

// ServiceInfo represents an available service on the network.
type ServiceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// APIServicesResponse is the response from the services query endpoint.
type APIServicesResponse struct {
	Service []APIServiceEntry `json:"service"`
}

// APIServiceEntry represents a single service from the API.
type APIServiceEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AutoTopUpConfig is the stored per-app auto-top-up configuration (uPOKT).
type AutoTopUpConfig struct {
	Enabled          bool  `json:"enabled"`
	TriggerThreshold int64 `json:"trigger_threshold"` // uPOKT
	TargetAmount     int64 `json:"target_amount"`     // uPOKT
}

// AutoTopUpRequest is the JSON body from the frontend (POKT values).
type AutoTopUpRequest struct {
	Enabled          bool    `json:"enabled"`
	TriggerThreshold float64 `json:"trigger_threshold"` // POKT
	TargetAmount     float64 `json:"target_amount"`     // POKT
}

// AutoTopUpEvent records a single auto-top-up action.
type AutoTopUpEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	Network       string    `json:"network"`
	Address       string    `json:"address"`
	PreviousStake int64     `json:"previous_stake"`
	TargetAmount  int64     `json:"target_amount"`
	FundTxHash    string    `json:"fund_tx_hash,omitempty"`
	StakeTxHash   string    `json:"stake_tx_hash,omitempty"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	Phase         string    `json:"phase"`
}
