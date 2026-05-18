package pocket

import (
	"fmt"
	"os"

	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/validate"
)

// Tx flags shared by every pocketd write SAM submits.
//
// --unordered + --timeout-duration replace the default ordered-by-sequence
// model with replay protection by tx hash + a window. This sidesteps the
// "account sequence mismatch, expected N, got M" race entirely.
//
// --gas=auto + --gas-adjustment 1.5 keeps the auto-simulation path (so we
// don't maintain a per-op gas table) but multiplies the simulated value
// by 1.5× to leave headroom. pocketd's simulator does not model the
// unordered-tx nonce-dedup store write — production hit this with
// simulated 50,959 vs actual 51,137 → code 11 out of gas. With 1.5×
// adjustment, simulated 50,959 becomes 76,438 broadcast gas: comfortable
// margin for the unordered store write plus any future chain growth.
const (
	txTimeoutDuration = "5m"
	txGasAdjustment   = "1.5"
)

// commonTxFlags returns the flag set every tx shares.
func (e *Executor) commonTxFlags() []string {
	flags := []string{
		"--yes",
		"--gas=auto",
		"--gas-adjustment", txGasAdjustment,
		"--fees=1upokt",
		"--output", "json",
		"--unordered",
		"--timeout-duration", txTimeoutDuration,
	}
	if e.Config.Config.KeyringBackend != "" {
		flags = append(flags, "--keyring-backend", e.Config.Config.KeyringBackend)
	}
	return flags
}

// StakeNewApplication stakes a new application with the given service ID and amount (in uPOKT).
func (e *Executor) StakeNewApplication(appAddress, serviceID, network string, amountUpokt int64, rpcEndpoint string) (*models.TransactionResponse, error) {
	if err := validate.ServiceID(serviceID); err != nil {
		return nil, fmt.Errorf("invalid service ID: %w", err)
	}

	amountStr := fmt.Sprintf("%dupokt", amountUpokt)

	e.Logger.Info("staking new application",
		"address", appAddress,
		"service_id", serviceID,
		"amount", amountStr,
	)

	stakeConfig, err := writeTempStakeConfig(amountStr, serviceID)
	if err != nil {
		return nil, err
	}
	defer os.Remove(stakeConfig)

	args := []string{
		"tx", "application", "stake-application",
		"--config", stakeConfig,
		"--from", appAddress,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("stake new app command", "args", args)
	return e.RunTx(args...)
}

// UpstakeApplication increases an application's stake by the given amount (in uPOKT).
func (e *Executor) UpstakeApplication(appAddress, network string, amount int64, rpcEndpoint, apiEndpoint string) (*models.TransactionResponse, error) {
	app, err := e.Client.QueryApplication(appAddress, apiEndpoint, network)
	if err != nil {
		return nil, fmt.Errorf("failed to query application before upstake: %w", err)
	}

	if app.ServiceID == "" {
		return nil, fmt.Errorf("application has no service ID configured")
	}

	if err := validate.ServiceID(app.ServiceID); err != nil {
		return nil, fmt.Errorf("unsafe service ID from API: %w", err)
	}

	newStakeAmount, err := validate.StakeAddition(app.Stake, amount)
	if err != nil {
		return nil, fmt.Errorf("invalid stake calculation: %w", err)
	}
	amountStr := fmt.Sprintf("%dupokt", newStakeAmount)

	e.Logger.Info("upstaking application",
		"address", appAddress,
		"current_stake", app.Stake,
		"adding", amount,
		"new_stake", newStakeAmount,
	)

	stakeConfig, err := writeTempStakeConfig(amountStr, app.ServiceID)
	if err != nil {
		return nil, err
	}
	defer os.Remove(stakeConfig)

	args := []string{
		"tx", "application", "stake-application",
		"--config", stakeConfig,
		"--from", appAddress,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("upstake command", "args", args)
	return e.RunTx(args...)
}

// UnstakeApplication begins the unbonding process for an application. After
// the unbonding period (~1 session on mainnet, ~1–2h), the staked POKT is
// returned to the application's liquid balance automatically and the on-chain
// entry is removed.
func (e *Executor) UnstakeApplication(appAddress, network, rpcEndpoint string) (*models.TransactionResponse, error) {
	e.Logger.Info("unstaking application", "address", appAddress, "network", network)

	args := []string{
		"tx", "application", "unstake-application",
		"--from", appAddress,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("unstake command", "args", args)
	return e.RunTx(args...)
}

// DelegateToGateway delegates an application to a gateway.
func (e *Executor) DelegateToGateway(appAddress, gatewayAddress, network, rpcEndpoint string) (*models.TransactionResponse, error) {
	e.Logger.Info("delegating to gateway", "app", appAddress, "gateway", gatewayAddress)

	args := []string{
		"tx", "application", "delegate-to-gateway",
		gatewayAddress,
		"--from", appAddress,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("delegate command", "args", args)
	return e.RunTx(args...)
}

// FundApplication sends POKT from the bank to an application address.
// Verified working with --unordered: concurrent self-sends from the same
// bank in one block all land without sequence-tracking gymnastics.
func (e *Executor) FundApplication(appAddress, bankAddress, network string, amount int64, rpcEndpoint string) (*models.TransactionResponse, error) {
	amountStr := fmt.Sprintf("%dupokt", amount)

	e.Logger.Info("funding application", "address", appAddress, "amount", amountStr)

	args := []string{
		"tx", "bank", "send",
		bankAddress,
		appAddress,
		amountStr,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("fund command", "args", args)
	return e.RunTx(args...)
}

// ReturnLiquidToBank sends `amount` uPOKT from an application's liquid
// balance back to the bank account, signed by the application itself.
// Used by the /api/applications/{addr}/return-liquid endpoint to sweep
// unused liquid POKT off an app while leaving a small reserve for
// future tx fees. Caller computes amount; this function does not
// reserve anything on its own.
func (e *Executor) ReturnLiquidToBank(appAddress, bankAddress, network string, amount int64, rpcEndpoint string) (*models.TransactionResponse, error) {
	amountStr := fmt.Sprintf("%dupokt", amount)

	e.Logger.Info("returning liquid to bank",
		"from_app", appAddress, "to_bank", bankAddress, "amount", amountStr)

	args := []string{
		"tx", "bank", "send",
		appAddress,
		bankAddress,
		amountStr,
		"--node", rpcEndpoint,
		"--chain-id", network,
	}
	args = append(args, e.commonTxFlags()...)

	e.Logger.Debug("return-liquid command", "args", args)
	return e.RunTx(args...)
}

// writeTempStakeConfig writes a temporary YAML config for stake-application and returns its path.
func writeTempStakeConfig(amount, serviceID string) (string, error) {
	tempFile, err := os.CreateTemp("", "pocketd-stake-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp config file: %w", err)
	}
	if err := tempFile.Chmod(0600); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to set temp file permissions: %w", err)
	}

	yamlContent := fmt.Sprintf("stake_amount: %s\nservice_ids:\n  - %s\n", amount, serviceID)
	if _, err := tempFile.WriteString(yamlContent); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to close temp config file: %w", err)
	}

	return tempFile.Name(), nil
}
