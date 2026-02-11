package models

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
