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
	maxEvents              = 100
	txFee                  = int64(1)         // 1 uPOKT fee for the upstake transaction
	defaultMinLiquidBalance = int64(1_000_000) // 1 POKT default reserve
)

// AppQuerier queries application state from the network.
type AppQuerier interface {
	QueryApplication(address, apiEndpoint, network string) (*models.Application, error)
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
// Runs an immediate check on startup, then every 3 minutes (~2.5 blocks
// at the current ~70s block time, comfortably within a single session).
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	w.Logger.Info("auto-top-up worker started")

	// Run immediately on startup instead of waiting for the first tick.
	w.RunOnce(ctx)

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
		w.Logger.Debug("auto-top-up: no enabled configs, skipping cycle")
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

	// Skip apps that are not actively staked. UNBONDING apps cannot be
	// upstaked (protocol rejects), and NOT_FOUND apps have nothing to top up
	// (either never staked or already finished unbonding — config tracking
	// row is kept, but a fresh stake-application tx is required to restart).
	if app.Status != models.AppStatusStaked {
		w.Logger.Debug("auto-top-up: app not in STAKED status, skipping",
			"address", address, "status", app.Status)
		return
	}

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

	// Two-phase approach: fund in one cycle, upstake in the next.
	// This lets the fund tx confirm on-chain before attempting the upstake.
	minLiquid := cfg.MinLiquidBalance
	if minLiquid <= 0 {
		minLiquid = defaultMinLiquidBalance
	}
	totalLiquidNeeded := amountNeeded + txFee + minLiquid
	fundAmount := totalLiquidNeeded - app.LiquidBalance

	if fundAmount > 0 {
		// Phase 1: Fund the app. Return and let the next cycle handle the upstake.
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
		event.Success = true
		w.addEvent(event)

		w.BankCache.Delete(network)
		w.Logger.Info("auto-top-up: funded, upstake will happen next cycle",
			"address", address, "tx_hash", fundResult.TxHash)
		return
	}

	// Phase 2: App has enough liquid (from a previous fund or existing balance). Upstake now.
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

	w.AppCache.Delete(network)
	w.BankCache.Delete(network)

	w.Logger.Info("auto-top-up: upstake complete",
		"address", address, "network", network, "tx_hash", stakeResult.TxHash)
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
