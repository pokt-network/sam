package pocket

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/validate"
)

// seqMismatchRE captures both expected and got from a Cosmos SDK
// "account sequence mismatch, expected N, got M: incorrect account sequence" error.
var seqMismatchRE = regexp.MustCompile(`account sequence mismatch, expected (\d+), got (\d+)`)

// ParseExpectedSequence extracts the "expected" sequence from a Cosmos
// sequence-mismatch error string. Returns (expected, true) on match,
// (0, false) otherwise.
func ParseExpectedSequence(s string) (uint64, bool) {
	m := seqMismatchRE.FindStringSubmatch(s)
	if len(m) != 3 {
		return 0, false
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("stake new app command", "args", args)
	return e.RunTxWithSeqRetry(appAddress, network, args)
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("upstake command", "args", args)
	return e.RunTxWithSeqRetry(appAddress, network, args)
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
		"--yes",
		"--gas=auto",
		"--fees=1upokt",
		"--output", "json",
	}

	if e.Config.Config.KeyringBackend != "" {
		args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
	}

	e.Logger.Debug("unstake command", "args", args)
	return e.RunTxWithSeqRetry(appAddress, network, args)
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
	return e.RunTxWithSeqRetry(appAddress, network, args)
}

// FundApplication sends POKT from the bank to an application address.
// Used for one-shot manual funds via the /api/applications/{addr}/fund
// endpoint where there's no in-flight neighbor tx that could race on the
// bank's sequence. The auto top-up worker uses FundApplicationWithSequence
// instead, which threads an explicit sequence through the cycle.
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
	return e.RunTxWithSeqRetry(bankAddress, network, args)
}

// FundApplicationWithSequence is FundApplication with explicit
// --account-number + --sequence flags, used by the auto top-up worker so
// multiple bank-signed fund txs in the same cycle don't race against the
// chain's account sequence.
//
// On a "account sequence mismatch, expected N, got M" failure, this
// function retries once with N. usedSeq in the return reflects the
// sequence that was actually broadcast (passed-in `sequence` if no
// retry, or the parsed `expected` if the retry kicked in), so the
// worker can resync its local counter with usedSeq+1.
func (e *Executor) FundApplicationWithSequence(
	appAddress, bankAddress, network string, amount int64, rpcEndpoint string,
	accountNumber, sequence uint64,
) (*models.TransactionResponse, uint64, error) {
	amountStr := fmt.Sprintf("%dupokt", amount)

	e.Logger.Info("funding application (sequenced)",
		"address", appAddress, "amount", amountStr,
		"account_number", accountNumber, "sequence", sequence)

	buildArgs := func(seq uint64) []string {
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
			"--account-number", strconv.FormatUint(accountNumber, 10),
			"--sequence", strconv.FormatUint(seq, 10),
		}
		if e.Config.Config.KeyringBackend != "" {
			args = append(args, "--keyring-backend", e.Config.Config.KeyringBackend)
		}
		return args
	}

	usedSeq := sequence
	args := buildArgs(usedSeq)
	e.Logger.Debug("fund command (sequenced)", "args", args)
	resp, err := e.RunTx(args...)

	errStr := ""
	if err != nil {
		errStr = err.Error()
	} else if resp != nil && !resp.Success {
		errStr = resp.Message
	}
	if expected, ok := ParseExpectedSequence(errStr); ok && expected != usedSeq {
		e.Logger.Warn("auto-top-up: sequence mismatch — retrying with chain-reported expected sequence",
			"address", appAddress, "had", usedSeq, "expected", expected)
		usedSeq = expected
		args = buildArgs(usedSeq)
		resp, err = e.RunTx(args...)
	}

	return resp, usedSeq, err
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
