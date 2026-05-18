package pocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/validate"
)

// Executor runs pocketd CLI commands for write transactions.
type Executor struct {
	Binary string
	Config *config.Config
	Client *Client
	Logger *slog.Logger
}

// NewExecutor returns an Executor that shells out to pocketd.
// Returns an error if the keyring backend is configured but invalid.
func NewExecutor(cfg *config.Config, client *Client, logger *slog.Logger) (*Executor, error) {
	if cfg.Config.KeyringBackend != "" {
		if err := validate.KeyringBackend(cfg.Config.KeyringBackend); err != nil {
			return nil, fmt.Errorf("invalid keyring backend: %w", err)
		}
	}
	return &Executor{
		Binary: "pocketd",
		Config: cfg,
		Client: client,
		Logger: logger,
	}, nil
}

// Run executes a pocketd command with the given arguments.
func (e *Executor) Run(args ...string) (string, error) {
	cmd := exec.Command(e.Binary, args...)
	cmd.Env = e.buildEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pocketd command failed: %s - %w", string(output), err)
	}

	return string(output), nil
}

// buildEnv constructs the environment variables for pocketd commands.
func (e *Executor) buildEnv() []string {
	env := []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	if e.Config.Config.PocketdHome != "" {
		env = append(env, "POCKETD_HOME="+e.Config.Config.PocketdHome)
	}
	if e.Config.Config.KeyringBackend != "" {
		env = append(env, "KEYRING_BACKEND="+e.Config.Config.KeyringBackend)
	}
	return env
}

// RunTxWithSeqRetry executes a pocketd transaction command and, if it
// fails with a Cosmos "account sequence mismatch" error, queries the
// signer's account from the chain and retries once with explicit
// --account-number and --sequence flags. This catches any race where
// two writes from the same signer are in flight near-simultaneously
// (worker cycles overlapping, manual API call racing the worker,
// admin-issued shell tx racing SAM, etc.) without requiring callers to
// pre-fetch per-signer sequence state.
//
// signer is the bech32 address whose key signs this tx (the --from
// value). network is used to look up the apiEndpoint for QueryAccount.
//
// Caller may pass args that already contain --account-number and/or
// --sequence (e.g. the auto top-up worker's sequenced-fund path); on
// retry those flags are replaced with the chain-reported expected
// values rather than appended a second time.
func (e *Executor) RunTxWithSeqRetry(signer, network string, args []string) (*models.TransactionResponse, error) {
	resp, err := e.RunTx(args...)

	errStr := ""
	if err != nil {
		errStr = err.Error()
	} else if resp != nil && !resp.Success {
		errStr = resp.Message
	}
	expected, ok := ParseExpectedSequence(errStr)
	if !ok {
		return resp, err
	}

	netCfg, ok := e.Config.Config.Networks[network]
	if !ok {
		e.Logger.Warn("tx sequence mismatch but network not in config — cannot retry",
			"signer", signer, "network", network, "expected", expected)
		return resp, err
	}

	if e.Client == nil {
		e.Logger.Warn("tx sequence mismatch but Executor has no Client — cannot retry",
			"signer", signer, "network", network)
		return resp, err
	}

	acc, accErr := e.Client.QueryAccount(signer, netCfg.APIEndpoint)
	if accErr != nil {
		e.Logger.Error("tx sequence mismatch but account lookup for retry failed",
			"signer", signer, "network", network, "expected", expected, "error", accErr)
		return resp, err
	}

	// The chain just told us the expected sequence is `expected`. Prefer
	// that over what QueryAccount returned (the lookup is racy too, and
	// the error message is authoritative). Use account_number from the
	// account query.
	retryArgs := setOrAppendFlag(args, "--account-number", strconv.FormatUint(acc.AccountNumber, 10))
	retryArgs = setOrAppendFlag(retryArgs, "--sequence", strconv.FormatUint(expected, 10))

	e.Logger.Warn("retrying tx after sequence mismatch",
		"signer", signer, "network", network,
		"account_number", acc.AccountNumber, "sequence", expected)

	return e.RunTx(retryArgs...)
}

// setOrAppendFlag returns a copy of args with `flag value` either
// replacing the existing occurrence or appended to the end. Used by the
// seq-mismatch retry to avoid duplicate --account-number/--sequence
// flags when the caller already supplied them.
func setOrAppendFlag(args []string, flag, value string) []string {
	out := make([]string, 0, len(args)+2)
	replaced := false
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			out = append(out, flag, value)
			i++ // skip old value
			replaced = true
			continue
		}
		out = append(out, args[i])
	}
	if !replaced {
		out = append(out, flag, value)
	}
	return out
}

// RunTx executes a pocketd transaction command and parses the result.
// It checks both the CLI exit code and the on-chain response code.
func (e *Executor) RunTx(args ...string) (*models.TransactionResponse, error) {
	output, err := e.Run(args...)
	if err != nil {
		return nil, err
	}

	// pocketd --gas=auto prints "gas estimate: N\n" before the JSON.
	// Extract the JSON object if the output has a prefix.
	jsonStr := output
	if idx := strings.Index(output, "{"); idx > 0 {
		jsonStr = output[idx:]
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		e.Logger.Warn("pocketd returned non-JSON output", "output", output)
		return &models.TransactionResponse{
			Success: false,
			Message: fmt.Sprintf("unexpected output: %.200s", output),
		}, nil
	}

	txhash, _ := result["txhash"].(string)

	// code == 0 means success; any other value is an on-chain failure.
	if code, ok := result["code"].(float64); ok && code != 0 {
		rawLog, _ := result["raw_log"].(string)
		if rawLog == "" {
			rawLog = fmt.Sprintf("tx failed with code %d", int(code))
		}
		return &models.TransactionResponse{TxHash: txhash, Success: false, Message: rawLog}, nil
	}

	return &models.TransactionResponse{TxHash: txhash, Success: true}, nil
}
