package pocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

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
func NewExecutor(cfg *config.Config, client *Client, logger *slog.Logger) *Executor {
	return &Executor{
		Binary: "pocketd",
		Config: cfg,
		Client: client,
		Logger: logger,
	}
}

// Run executes a pocketd command with the given arguments.
func (e *Executor) Run(args ...string) (string, error) {
	cmd := exec.Command(e.Binary, args...)

	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}

	if e.Config.Config.PocketdHome != "" {
		cmd.Env = append(cmd.Env, "POCKETD_HOME="+e.Config.Config.PocketdHome)
	}

	if e.Config.Config.KeyringBackend != "" {
		if err := validate.KeyringBackend(e.Config.Config.KeyringBackend); err != nil {
			return "", fmt.Errorf("invalid keyring backend: %w", err)
		}
		cmd.Env = append(cmd.Env, "KEYRING_BACKEND="+e.Config.Config.KeyringBackend)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pocketd command failed: %s - %w", string(output), err)
	}

	return string(output), nil
}

// RunTx executes a pocketd transaction command and parses the result.
// It checks both the CLI exit code and the on-chain response code.
func (e *Executor) RunTx(args ...string) (*models.TransactionResponse, error) {
	output, err := e.Run(args...)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return &models.TransactionResponse{Success: true, Message: "Transaction submitted"}, nil
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
