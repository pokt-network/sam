package autotopup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/pocket"
)

const (
	maxEvents       = 100
	pollInterval    = 10 * time.Second
	pollMaxAttempts = 6
)

// Worker runs periodic auto-top-up checks.
type Worker struct {
	Store     *Store
	Config    *config.Config
	Client    *pocket.Client
	Executor  *pocket.Executor
	AppCache  *cache.Cache[[]models.Application]
	BankCache *cache.Cache[models.BankAccount]
	Logger    *slog.Logger

	mu     sync.Mutex
	events []models.AutoTopUpEvent
}

// NewWorker creates a new auto-top-up worker.
func NewWorker(store *Store, cfg *config.Config, client *pocket.Client, executor *pocket.Executor, appCache *cache.Cache[[]models.Application], bankCache *cache.Cache[models.BankAccount], logger *slog.Logger) *Worker {
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
			w.RunOnce()
		}
	}
}

// RunOnce performs a single cycle of auto-top-up checks.
func (w *Worker) RunOnce() {
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
		netCfg, ok := w.Config.Config.Networks[network]
		if !ok {
			w.Logger.Warn("auto-top-up: unknown network", "network", network)
			continue
		}

		for address, cfg := range apps {
			w.processApp(network, address, cfg, netCfg)
		}
	}

	w.Logger.Info("auto-top-up cycle complete")
}

func (w *Worker) processApp(network, address string, cfg models.AutoTopUpConfig, netCfg config.NetworkConfig) {
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
	fundAmount := amountNeeded - app.LiquidBalance
	if fundAmount > 0 {
		event.Phase = "fund"
		w.Logger.Info("auto-top-up: funding app from bank",
			"address", address, "fund_amount", fundAmount)

		fundResult, err := w.Executor.FundApplication(address, netCfg.Bank, network, fundAmount, netCfg.RPCEndpoint)
		if err != nil || !fundResult.Success {
			errMsg := "fund failed"
			if err != nil {
				errMsg = err.Error()
			}
			w.Logger.Error("auto-top-up: fund failed", "address", address, "error", errMsg)
			event.Error = errMsg
			w.addEvent(event)
			return
		}
		event.FundTxHash = fundResult.TxHash

		// Poll for balance confirmation.
		if !w.pollBalance(address, netCfg.APIEndpoint, app.LiquidBalance+fundAmount) {
			w.Logger.Warn("auto-top-up: balance not confirmed after polling, proceeding anyway", "address", address)
		}
	} else {
		w.Logger.Info("auto-top-up: app has sufficient liquid balance, skipping fund", "address", address)
	}

	// Upstake to the target amount.
	event.Phase = "upstake"
	w.Logger.Info("auto-top-up: upstaking app",
		"address", address, "amount", amountNeeded)

	stakeResult, err := w.Executor.UpstakeApplication(address, netCfg.Bank, network, amountNeeded, netCfg.RPCEndpoint, netCfg.APIEndpoint)
	if err != nil || !stakeResult.Success {
		errMsg := "upstake failed"
		if err != nil {
			errMsg = err.Error()
		}
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

func (w *Worker) pollBalance(address, apiEndpoint string, minBalance int64) bool {
	for i := 0; i < pollMaxAttempts; i++ {
		time.Sleep(pollInterval)
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

func (w *Worker) addEvent(event models.AutoTopUpEvent) {
	w.events = append(w.events, event)
	if len(w.events) > maxEvents {
		w.events = w.events[len(w.events)-maxEvents:]
	}
}

// Events returns a copy of recent events.
func (w *Worker) Events() []models.AutoTopUpEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := make([]models.AutoTopUpEvent, len(w.events))
	copy(result, w.events)
	return result
}
