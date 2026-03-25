package autotopup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
)

const (
	maxEvents       = 100
	pollMaxAttempts = 6
	txFee           = int64(1) // 1 uPOKT fee for the upstake transaction
)

// PollInterval is the delay between balance confirmation polls.
// Exported to allow tests to override.
var PollInterval = 10 * time.Second

// AppQuerier queries application state from the network.
type AppQuerier interface {
	QueryApplication(address, apiEndpoint, network string) (*models.Application, error)
	QueryBalance(address, apiEndpoint string) (int64, error)
}

// TxExecutor executes on-chain transactions.
type TxExecutor interface {
	FundApplication(appAddress, bankAddress, network string, amount int64, rpcEndpoint string) (*models.TransactionResponse, error)
	UpstakeApplication(appAddress, network string, amount int64, rpcEndpoint, apiEndpoint string) (*models.TransactionResponse, error)
}

// Worker runs periodic auto-top-up checks.
type Worker struct {
	Store     *Store
	Config    *config.Config
	Client    AppQuerier
	Executor  TxExecutor
	AppCache  *cache.Cache[[]models.Application]
	BankCache *cache.Cache[models.BankAccount]
	Logger    *slog.Logger

	mu       sync.Mutex
	eventsMu sync.Mutex
	events   []models.AutoTopUpEvent
}

// NewWorker creates a new auto-top-up worker.
func NewWorker(store *Store, cfg *config.Config, client AppQuerier, executor TxExecutor, appCache *cache.Cache[[]models.Application], bankCache *cache.Cache[models.BankAccount], logger *slog.Logger) *Worker {
	return &Worker{
		Store:     store,
		Config:    cfg,
		Client:    client,
		Executor:  executor,
		AppCache:  appCache,
		BankCache: bankCache,
		Logger:    logger,
		events:    make([]models.AutoTopUpEvent, 0, maxEvents),
	}
}

// Run starts the worker loop. It blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	w.Logger.Info("auto-top-up worker started")

	for {
		select {
		case <-ctx.Done():
			w.Logger.Info("auto-top-up worker stopped")
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce performs a single cycle of auto-top-up checks.
func (w *Worker) RunOnce(ctx context.Context) {
	if !w.mu.TryLock() {
		w.Logger.Warn("auto-top-up cycle already in progress, skipping")
		return
	}
	defer w.mu.Unlock()

	enabled := w.Store.GetEnabled()
	if len(enabled) == 0 {
		return
	}

	w.Logger.Info("auto-top-up cycle starting", "networks", len(enabled))

	for network, apps := range enabled {
		if ctx.Err() != nil {
			w.Logger.Info("auto-top-up cycle cancelled")
			return
		}

		netCfg, ok := w.Config.Config.Networks[network]
		if !ok {
			w.Logger.Warn("auto-top-up: unknown network", "network", network)
			continue
		}

		for address, cfg := range apps {
			if ctx.Err() != nil {
				w.Logger.Info("auto-top-up cycle cancelled")
				return
			}
			w.processApp(ctx, network, address, cfg, netCfg)
		}
	}

	w.Logger.Info("auto-top-up cycle complete")
}

func (w *Worker) processApp(ctx context.Context, network, address string, cfg models.AutoTopUpConfig, netCfg config.NetworkConfig) {
	event := models.AutoTopUpEvent{
		Timestamp:    time.Now(),
		Network:      network,
		Address:      address,
		TargetAmount: cfg.TargetAmount,
		Phase:        "check",
	}

	app, err := w.Client.QueryApplication(address, netCfg.APIEndpoint, network)
	if err != nil {
		w.Logger.Error("auto-top-up: failed to query app", "address", address, "error", err)
		event.Error = err.Error()
		w.addEvent(event)
		return
	}

	event.PreviousStake = app.Stake

	if app.Stake >= cfg.TriggerThreshold {
		w.Logger.Debug("auto-top-up: stake above threshold, skipping",
			"address", address, "stake", app.Stake, "threshold", cfg.TriggerThreshold)
		return
	}

	amountNeeded := cfg.TargetAmount - app.Stake
	if amountNeeded <= 0 {
		return
	}

	w.Logger.Info("auto-top-up: app needs top-up",
		"address", address,
		"current_stake", app.Stake,
		"target", cfg.TargetAmount,
		"amount_needed", amountNeeded,
	)

	// Smart funding: check if the app already has enough liquid balance.
	// The app needs: amountNeeded (for stake increase) + txFee + minLiquidBalance (reserve).
	totalLiquidNeeded := amountNeeded + txFee + cfg.MinLiquidBalance
	fundAmount := totalLiquidNeeded - app.LiquidBalance
	if fundAmount > 0 {
		event.Phase = "fund"
		event.FundAmount = fundAmount
		w.Logger.Info("auto-top-up: funding app from bank",
			"address", address, "fund_amount", fundAmount)

		fundResult, err := w.Executor.FundApplication(address, netCfg.Bank, network, fundAmount, netCfg.RPCEndpoint)
		if errMsg := txErrMsg("fund failed", fundResult, err); errMsg != "" {
			w.Logger.Error("auto-top-up: fund failed", "address", address, "error", errMsg)
			event.Error = errMsg
			w.addEvent(event)
			return
		}
		event.FundTxHash = fundResult.TxHash

		// Poll for balance confirmation.
		if !w.pollBalance(ctx, address, netCfg.APIEndpoint, app.LiquidBalance+fundAmount) {
			w.Logger.Warn("auto-top-up: balance not confirmed after polling, proceeding anyway", "address", address)
		}
	} else {
		w.Logger.Info("auto-top-up: app has sufficient liquid balance, skipping fund", "address", address)
	}

	// Upstake to the target amount.
	event.Phase = "upstake"
	w.Logger.Info("auto-top-up: upstaking app",
		"address", address, "amount", amountNeeded)

	stakeResult, err := w.Executor.UpstakeApplication(address, network, amountNeeded, netCfg.RPCEndpoint, netCfg.APIEndpoint)
	if errMsg := txErrMsg("upstake failed", stakeResult, err); errMsg != "" {
		w.Logger.Error("auto-top-up: upstake failed", "address", address, "error", errMsg)
		event.Error = errMsg
		w.addEvent(event)
		return
	}
	event.StakeTxHash = stakeResult.TxHash

	event.Phase = "complete"
	event.Success = true
	w.addEvent(event)

	// Invalidate caches.
	w.AppCache.Delete(network)
	w.BankCache.Delete(network)

	w.Logger.Info("auto-top-up: success", "address", address, "network", network)
}

func (w *Worker) pollBalance(ctx context.Context, address, apiEndpoint string, minBalance int64) bool {
	for i := 0; i < pollMaxAttempts; i++ {
		select {
		case <-ctx.Done():
			w.Logger.Info("auto-top-up: poll cancelled during shutdown")
			return false
		case <-time.After(PollInterval):
		}
		balance, err := w.Client.QueryBalance(address, apiEndpoint)
		if err != nil {
			w.Logger.Warn("auto-top-up: poll balance error", "attempt", i+1, "error", err)
			continue
		}
		if balance >= minBalance {
			return true
		}
		w.Logger.Debug("auto-top-up: balance not yet confirmed",
			"attempt", i+1, "current", balance, "required", minBalance)
	}
	return false
}

// txErrMsg returns an error message if the transaction failed, or "" on success.
func txErrMsg(fallback string, result *models.TransactionResponse, err error) string {
	if err != nil {
		return err.Error()
	}
	if result != nil && !result.Success {
		if result.Message != "" {
			return result.Message
		}
		return fallback
	}
	return ""
}

func (w *Worker) addEvent(event models.AutoTopUpEvent) {
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()

	w.events = append(w.events, event)
	if len(w.events) > maxEvents {
		w.events = w.events[len(w.events)-maxEvents:]
	}
}

// Events returns a copy of recent events.
func (w *Worker) Events() []models.AutoTopUpEvent {
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()

	result := make([]models.AutoTopUpEvent, len(w.events))
	copy(result, w.events)
	return result
}
