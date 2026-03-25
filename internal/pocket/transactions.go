package pocket

import (
	"fmt"
	"os"

	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/validate"
)

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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("stake new app command", "args", args)
	return e.RunTx(args...)
}

// UpstakeApplication increases an application's stake by the given amount (in uPOKT).
func (e *Executor) UpstakeApplication(appAddress, bankAddress, network string, amount int64, rpcEndpoint, apiEndpoint string) (*models.TransactionResponse, error) {
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("upstake command", "args", args)
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("delegate command", "args", args)
	return e.RunTx(args...)
}

// FundApplication sends POKT from the bank to an application address.
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("fund command", "args", args)
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
