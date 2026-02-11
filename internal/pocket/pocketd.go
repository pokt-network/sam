package pocket

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/pokt-network/sam/internal/config"
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

// parseTxHash attempts to extract the txhash from JSON output.
func parseTxHash(output string) string {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		if txhash, ok := result["txhash"].(string); ok {
			return txhash
		}
	}
	return ""
}
