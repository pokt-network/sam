package autotopup

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
	"github.com/pokt-network/sam/internal/notify"
)

const (
	maxEvents              = 100
	txFee                  = int64(1)         // 1 uPOKT fee for the upstake transaction
	defaultMinLiquidBalance = int64(1_000_000) // 1 POKT default reserve
)

// AppQuerier queries application state, bank balances, and account info
// from the network.
type AppQuerier interface {
	QueryApplication(address, apiEndpoint, network string) (*models.Application, error)
	QueryBalance(address, apiEndpoint string) (int64, error)
	QueryAccount(address, apiEndpoint string) (*models.AccountInfo, error)
}

// TxExecutor executes on-chain transactions.
//
// FundApplicationWithSequence is the version the worker uses for in-cycle
// bank-signed fund txs: it takes an explicit account_number + sequence so
// successive fund txs in the same cycle don't race on the chain's
// sequence counter. Returns (response, sequenceActuallyUsed, error) — the
// caller increments its local sequence based on usedSeq so the on-mismatch
// retry doesn't desync the cycle.
type TxExecutor interface {
	FundApplication(appAddress, bankAddress, network string, amount int64, rpcEndpoint string) (*models.TransactionResponse, error)
	FundApplicationWithSequence(appAddress, bankAddress, network string, amount int64, rpcEndpoint string, accountNumber, sequence uint64) (*models.TransactionResponse, uint64, error)
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
	Notifier  *notify.Discord
	Logger    *slog.Logger

	mu       sync.Mutex
	eventsMu sync.Mutex
	events   []models.AutoTopUpEvent

	// bankStatusMu protects lastBankSufficient.
	bankStatusMu       sync.RWMutex
	lastBankSufficient map[string]bankStatus
}

// bankStatus snapshots whether the bank can cover pending top-ups for a
// network at the last cycle. Surfaced via BankStatus() for the frontend
// low-balance badge.
type bankStatus struct {
	Balance    int64
	Needed     int64
	Sufficient bool
	CheckedAt  time.Time
}

// NewWorker creates a new auto-top-up worker.
func NewWorker(store *Store, cfg *config.Config, client AppQuerier, executor TxExecutor, appCache *cache.Cache[[]models.Application], bankCache *cache.Cache[models.BankAccount], notifier *notify.Discord, logger *slog.Logger) *Worker {
	return &Worker{
		Store:              store,
		Config:             cfg,
		Client:             client,
		Executor:           executor,
		AppCache:           appCache,
		BankCache:          bankCache,
		Notifier:           notifier,
		Logger:             logger,
		events:             make([]models.AutoTopUpEvent, 0, maxEvents),
		lastBankSufficient: make(map[string]bankStatus),
	}
}

// BankStatus returns the most recent per-network bank sufficiency snapshot
// (whether the bank can cover all pending auto top-ups). Used by the
// frontend to render a "LOW" badge on the bank balance card.
func (w *Worker) BankStatus() map[string]models.BankStatus {
	w.bankStatusMu.RLock()
	defer w.bankStatusMu.RUnlock()
	out := make(map[string]models.BankStatus, len(w.lastBankSufficient))
	for network, s := range w.lastBankSufficient {
		out[network] = models.BankStatus{
			Network:    network,
			Balance:    s.Balance,
			Needed:     s.Needed,
			Sufficient: s.Sufficient,
			CheckedAt:  s.CheckedAt,
		}
	}
	return out
}

func (w *Worker) setBankStatus(network string, s bankStatus) {
	w.bankStatusMu.Lock()
	w.lastBankSufficient[network] = s
	w.bankStatusMu.Unlock()
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

		w.processNetwork(ctx, network, apps, netCfg)
	}

	w.Logger.Info("auto-top-up cycle complete")
}

// processNetwork pre-flights the bank balance once per network and then
// iterates each app, tracking a running deduction so an early large fund
// doesn't silently starve later apps. If the running deduction would
// exceed the bank balance, remaining apps are skipped with a logged WARN
// and a Discord notification (cooldown-gated) is fired.
func (w *Worker) processNetwork(ctx context.Context, network string, apps map[string]models.AutoTopUpConfig, netCfg config.NetworkConfig) {
	bankBalance, bankErr := w.Client.QueryBalance(netCfg.Bank, netCfg.APIEndpoint)
	if bankErr != nil {
		w.Logger.Error("auto-top-up: failed to query bank balance — skipping pre-flight",
			"network", network, "bank", netCfg.Bank, "error", bankErr)
		// Continue without pre-flight or sequence threading. Per-app fund
		// tx will surface chain errors if the bank really is empty, and
		// nil bankAccount tells processApp to fall back to the
		// (sequence-racing) plain FundApplication path.
		for address, cfg := range apps {
			if ctx.Err() != nil {
				return
			}
			w.processApp(ctx, network, address, cfg, netCfg, -1, nil)
		}
		return
	}

	// Pre-fetch the bank's account_number + sequence once per cycle, then
	// thread an explicit, locally-incremented sequence through each fund
	// tx so we don't race ourselves on the chain's sequence counter.
	bankAccount, accErr := w.Client.QueryAccount(netCfg.Bank, netCfg.APIEndpoint)
	if accErr != nil {
		w.Logger.Error("auto-top-up: failed to query bank account info — fund txs will race on sequence this cycle",
			"network", network, "bank", netCfg.Bank, "error", accErr)
		// bankAccount stays nil; processApp falls back to FundApplication.
	} else {
		w.Logger.Debug("auto-top-up: pre-fetched bank account info",
			"network", network, "account_number", bankAccount.AccountNumber, "sequence", bankAccount.Sequence)
	}

	remaining := bankBalance
	totalNeeded := int64(0)
	skipped := false

	for address, cfg := range apps {
		if ctx.Err() != nil {
			return
		}
		consumed := w.processApp(ctx, network, address, cfg, netCfg, remaining, bankAccount)
		if consumed < 0 {
			// Insufficient funds — processApp logged the skip. Track demand
			// so we can surface the total deficit in the alert.
			totalNeeded += -consumed
			skipped = true
			continue
		}
		remaining -= consumed
		totalNeeded += consumed
	}

	w.setBankStatus(network, bankStatus{
		Balance:    bankBalance,
		Needed:     totalNeeded,
		Sufficient: !skipped,
		CheckedAt:  time.Now(),
	})

	if skipped {
		deficit := totalNeeded - bankBalance
		w.Logger.Warn("auto-top-up: bank cannot cover pending top-ups",
			"network", network,
			"bank_balance_upokt", bankBalance,
			"needed_upokt", totalNeeded,
			"deficit_upokt", deficit,
		)
		if w.Notifier.Enabled() {
			err := w.Notifier.Notify(ctx, network, map[string]string{
				"network":      network,
				"balance":      formatPOKT(bankBalance),
				"needed":       formatPOKT(totalNeeded),
				"deficit":      formatPOKT(deficit),
				"bank_address": netCfg.Bank,
			})
			if err != nil {
				w.Logger.Error("auto-top-up: discord notify failed", "network", network, "error", err)
			}
		}
	}
}

// formatPOKT formats a uPOKT amount as POKT (6 decimal places) for human
// display in Discord messages.
func formatPOKT(upokt int64) string {
	whole := upokt / 1_000_000
	frac := upokt % 1_000_000
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	return fmt.Sprintf("%d.%06d", whole, frac)
}

// processApp evaluates a single app for top-up.
//
// bankRemaining is the running bank balance the caller is willing to spend
// this cycle. Pass -1 to disable the pre-flight check (e.g. when the bank
// query itself failed).
//
// bankAccount carries the bank's account_number plus a mutable sequence
// counter shared across the cycle. When non-nil, fund txs use the
// sequence-explicit path and bump bankAccount.Sequence on each broadcast.
// When nil (account-info query failed), the legacy FundApplication is
// used and may race on the chain sequence — best-effort fallback.
//
// Return value:
//
//	>0  — uPOKT consumed from the bank for the fund tx this cycle
//	 0  — no spend (skipped, above threshold, or phase-2 upstake)
//	<0  — insufficient bank funds; absolute value is the uPOKT needed
func (w *Worker) processApp(ctx context.Context, network, address string, cfg models.AutoTopUpConfig, netCfg config.NetworkConfig, bankRemaining int64, bankAccount *models.AccountInfo) int64 {
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
		return 0
	}

	event.PreviousStake = app.Stake
	event.ServiceID = app.ServiceID

	// Skip apps that are not actively staked. UNBONDING apps *could* be
	// upstaked — the protocol cancels unbonding on any stake increase — but
	// unstaking is a deliberate operator action, so automation must not undo
	// it silently (cancel manually from the UI instead). NOT_FOUND apps have
	// nothing to top up (either never staked or already finished unbonding —
	// config tracking row is kept, but a fresh stake-application tx is
	// required to restart).
	if app.Status != models.AppStatusStaked {
		w.Logger.Debug("auto-top-up: app not in STAKED status, skipping",
			"address", address, "status", app.Status)
		return 0
	}

	if app.Stake >= cfg.TriggerThreshold {
		w.Logger.Debug("auto-top-up: stake above threshold, skipping",
			"address", address, "stake", app.Stake, "threshold", cfg.TriggerThreshold)
		return 0
	}

	amountNeeded := cfg.TargetAmount - app.Stake
	if amountNeeded <= 0 {
		return 0
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
		// Pre-flight: skip if the running bank balance can't cover this fund tx.
		// bankRemaining == -1 means the caller couldn't check, so let the chain
		// reject the tx itself (preserves prior behavior).
		if bankRemaining >= 0 && fundAmount > bankRemaining {
			w.Logger.Warn("auto-top-up: skipping fund — bank balance insufficient",
				"address", address, "fund_amount_upokt", fundAmount, "bank_remaining_upokt", bankRemaining)
			event.Phase = "fund"
			event.FundAmount = fundAmount
			event.Error = fmt.Sprintf("bank balance insufficient: need %d uPOKT, have %d uPOKT", fundAmount, bankRemaining)
			w.addEvent(event)
			return -fundAmount
		}

		// Phase 1: Fund the app. Return and let the next cycle handle the upstake.
		event.Phase = "fund"
		event.FundAmount = fundAmount
		w.Logger.Info("auto-top-up: funding app from bank",
			"address", address, "fund_amount", fundAmount)

		var (
			fundResult *models.TransactionResponse
			err        error
		)
		if bankAccount != nil {
			var usedSeq uint64
			fundResult, usedSeq, err = w.Executor.FundApplicationWithSequence(
				address, netCfg.Bank, network, fundAmount, netCfg.RPCEndpoint,
				bankAccount.AccountNumber, bankAccount.Sequence,
			)
			// Bump local sequence whenever pocketd actually broadcast (got a
			// non-empty txhash back). Even an on-chain failure consumes the
			// sequence — only a pre-broadcast error leaves it unused.
			if fundResult != nil && fundResult.TxHash != "" {
				bankAccount.Sequence = usedSeq + 1
			}
		} else {
			fundResult, err = w.Executor.FundApplication(address, netCfg.Bank, network, fundAmount, netCfg.RPCEndpoint)
		}
		if errMsg := txErrMsg("fund failed", fundResult, err); errMsg != "" {
			w.Logger.Error("auto-top-up: fund failed", "address", address, "error", errMsg)
			event.Error = errMsg
			w.addEvent(event)
			return 0
		}
		event.FundTxHash = fundResult.TxHash
		event.Success = true
		w.addEvent(event)

		w.BankCache.Delete(network)
		w.Logger.Info("auto-top-up: funded, upstake will happen next cycle",
			"address", address, "tx_hash", fundResult.TxHash)
		return fundAmount
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
		return 0
	}
	event.StakeTxHash = stakeResult.TxHash

	event.Phase = "complete"
	event.Success = true
	w.addEvent(event)

	w.AppCache.Delete(network)
	w.BankCache.Delete(network)

	w.Logger.Info("auto-top-up: upstake complete",
		"address", address, "network", network, "tx_hash", stakeResult.TxHash)
	return 0
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
