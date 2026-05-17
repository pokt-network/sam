package autotopup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/pokt-network/sam/internal/cache"
	"github.com/pokt-network/sam/internal/config"
	"github.com/pokt-network/sam/internal/models"
)

// mockClient implements AppQuerier for testing.
type mockClient struct {
	app *models.Application
	err error
}

func (m *mockClient) QueryApplication(address, apiEndpoint, network string) (*models.Application, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.app, nil
}

// mockExecutor implements TxExecutor and records the calls made.
type mockExecutor struct {
	fundCalls    []fundCall
	upstakeCalls []upstakeCall
	fundErr      error
	upstakeErr   error
}

type fundCall struct {
	AppAddress string
	Amount     int64
}

type upstakeCall struct {
	AppAddress string
	Amount     int64
}

func (m *mockExecutor) FundApplication(appAddress, bankAddress, network string, amount int64, rpcEndpoint string) (*models.TransactionResponse, error) {
	m.fundCalls = append(m.fundCalls, fundCall{AppAddress: appAddress, Amount: amount})
	if m.fundErr != nil {
		return nil, m.fundErr
	}
	return &models.TransactionResponse{TxHash: "fund-hash", Success: true}, nil
}

func (m *mockExecutor) UpstakeApplication(appAddress, network string, amount int64, rpcEndpoint, apiEndpoint string) (*models.TransactionResponse, error) {
	m.upstakeCalls = append(m.upstakeCalls, upstakeCall{AppAddress: appAddress, Amount: amount})
	if m.upstakeErr != nil {
		return nil, m.upstakeErr
	}
	return &models.TransactionResponse{TxHash: "upstake-hash", Success: true}, nil
}

func newTestWorker(t *testing.T, client AppQuerier, executor TxExecutor) *Worker {
	t.Helper()
	store, err := NewStore(tempStorePath(t))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	cfg := &config.Config{}
	cfg.Config.Networks = map[string]config.NetworkConfig{
		"testnet": {
			RPCEndpoint: "http://rpc",
			APIEndpoint: "http://api",
			Bank:        "pokt1bank00000000000000000000000000000000000",
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	appCache := cache.New[[]models.Application](time.Minute)
	bankCache := cache.New[models.BankAccount](time.Minute)
	return NewWorker(store, cfg, client, executor, appCache, bankCache, logger)
}

const (
	testAddr    = "pokt1app000000000000000000000000000000000000"
	testNetwork = "testnet"
	uPOKT      = int64(1_000_000) // 1 POKT in uPOKT
)

func TestProcessApp_FundOnly_NoUpstake(t *testing.T) {
	// App has 100 POKT staked, 0 liquid. Target is 200 POKT.
	// Phase 1: should fund and return (no upstake in same cycle).
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         100 * uPOKT,
			LiquidBalance: 0,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	// Fund should be called
	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(100*uPOKT + 1 + 1*uPOKT) // amountNeeded + txFee + default 1 POKT reserve
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}

	// Upstake should NOT be called — deferred to next cycle
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected 0 upstake calls (deferred to next cycle), got %d", len(executor.upstakeCalls))
	}

	events := w.Events()
	if len(events) != 1 || !events[0].Success || events[0].Phase != "fund" {
		t.Errorf("expected 1 success fund event, got %v", events)
	}
}

func TestProcessApp_MinLiquidBalanceReserve(t *testing.T) {
	// App has 100 POKT staked, 0 liquid. Target is 200 POKT.
	// MinLiquidBalance = 5 POKT. Fund should cover amountNeeded + txFee + reserve.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         100 * uPOKT,
			LiquidBalance: 0,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
		MinLiquidBalance: 5 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	// Fund = amountNeeded(100 POKT) + txFee(1) + reserve(5 POKT)
	expectedFund := int64(100*uPOKT + 1 + 5*uPOKT)
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}

	// No upstake in fund cycle
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected 0 upstake calls in fund cycle, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessApp_UpstakeWhenLiquidSufficient(t *testing.T) {
	// Phase 2: App has 100 POKT staked, 110 POKT liquid (from prior fund).
	// Should skip fund and upstake directly.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         100 * uPOKT,
			LiquidBalance: 110 * uPOKT,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
		MinLiquidBalance: 5 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 0 {
		t.Errorf("expected 0 fund calls, got %d", len(executor.fundCalls))
	}
	if len(executor.upstakeCalls) != 1 {
		t.Fatalf("expected 1 upstake call, got %d", len(executor.upstakeCalls))
	}
	if executor.upstakeCalls[0].Amount != 100*uPOKT {
		t.Errorf("upstake amount = %d, want %d", executor.upstakeCalls[0].Amount, 100*uPOKT)
	}

	events := w.Events()
	if len(events) != 1 || !events[0].Success || events[0].Phase != "complete" {
		t.Errorf("expected 1 success complete event, got %v", events)
	}
}

func TestProcessApp_PartialLiquidBalance(t *testing.T) {
	// App has 100 POKT staked, 50 POKT liquid. Target is 200 POKT.
	// MinLiquidBalance = 5 POKT. Not enough liquid — should fund only.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         100 * uPOKT,
			LiquidBalance: 50 * uPOKT,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
		MinLiquidBalance: 5 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(100*uPOKT + 1 + 5*uPOKT - 50*uPOKT)
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected 0 upstake calls in fund cycle, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessApp_StakeAboveThreshold_Skips(t *testing.T) {
	client := &mockClient{
		app: &models.Application{
			Address:   testAddr,
			ServiceID: "svc1",
			Status:    models.AppStatusStaked,
			Stake:     200 * uPOKT,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 0 {
		t.Errorf("expected no fund calls when stake above threshold, got %d", len(executor.fundCalls))
	}
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected no upstake calls when stake above threshold, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessApp_SkipsUnbondingApp(t *testing.T) {
	// App is mid-unbond. Worker must not attempt fund or upstake — protocol
	// rejects upstake on UNBONDING apps; spamming would just generate errors.
	client := &mockClient{
		app: &models.Application{
			Address:                 testAddr,
			ServiceID:               "svc1",
			Status:                  models.AppStatusUnbonding,
			Stake:                   100 * uPOKT,
			LiquidBalance:           0,
			UnstakeSessionEndHeight: 12345,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 0 {
		t.Errorf("expected no fund calls for UNBONDING app, got %d", len(executor.fundCalls))
	}
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected no upstake calls for UNBONDING app, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessApp_SkipsNotFoundApp(t *testing.T) {
	// App was unbonded and removed from on-chain state. Tracking row persists
	// in config.yaml but a fresh stake-application tx is required — worker
	// must not try to fund/upstake.
	client := &mockClient{
		app: &models.Application{
			Address: testAddr,
			Status:  models.AppStatusNotFound,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 0 {
		t.Errorf("expected no fund calls for NOT_FOUND app, got %d", len(executor.fundCalls))
	}
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected no upstake calls for NOT_FOUND app, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessApp_FundFails_NoUpstake(t *testing.T) {
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         100 * uPOKT,
			LiquidBalance: 0,
		},
	}
	executor := &mockExecutor{fundErr: fmt.Errorf("insufficient bank balance")}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 150 * uPOKT,
		TargetAmount:     200 * uPOKT,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected no upstake calls after fund failure, got %d", len(executor.upstakeCalls))
	}

	events := w.Events()
	if len(events) != 1 || events[0].Success {
		t.Errorf("expected 1 failure event, got %d events", len(events))
	}
	if events[0].Error == "" {
		t.Error("expected error message in event")
	}
}

func TestProcessApp_ZeroMinLiquid_DefaultsTo1POKT(t *testing.T) {
	// With MinLiquidBalance=0, defaults to 1 POKT reserve.
	// fund = amountNeeded + txFee + 1 POKT (default) - liquid
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Status:        models.AppStatusStaked,
			Stake:         180 * uPOKT,
			LiquidBalance: 10 * uPOKT,
		},
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 190 * uPOKT,
		TargetAmount:     200 * uPOKT,
		MinLiquidBalance: 0,
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(20*uPOKT + 1 + 1*uPOKT - 10*uPOKT) // amountNeeded + txFee + default 1 POKT - liquid
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}
}
