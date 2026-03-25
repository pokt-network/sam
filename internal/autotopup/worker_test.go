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
	app     *models.Application
	balance int64
	err     error
}

func (m *mockClient) QueryApplication(address, apiEndpoint, network string) (*models.Application, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.app, nil
}

func (m *mockClient) QueryBalance(address, apiEndpoint string) (int64, error) {
	return m.balance, nil
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

func init() {
	PollInterval = 1 * time.Millisecond
}

func TestProcessApp_FundAmountAccountsForTxFee(t *testing.T) {
	// App has 100 POKT staked, 0 liquid. Target is 200 POKT.
	// amountNeeded = 100 POKT, plus 1 uPOKT tx fee.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Stake:         100 * uPOKT,
			LiquidBalance: 0,
		},
		balance: 200 * uPOKT, // poll returns enough
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

	// Fund should cover amountNeeded + txFee = 100 POKT + 1 uPOKT
	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(100*uPOKT + 1) // amountNeeded + txFee
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}

	// Upstake should be called with amountNeeded
	if len(executor.upstakeCalls) != 1 {
		t.Fatalf("expected 1 upstake call, got %d", len(executor.upstakeCalls))
	}
	if executor.upstakeCalls[0].Amount != 100*uPOKT {
		t.Errorf("upstake amount = %d, want %d", executor.upstakeCalls[0].Amount, 100*uPOKT)
	}

	// Should record a success event
	events := w.Events()
	if len(events) != 1 || !events[0].Success {
		t.Errorf("expected 1 success event, got %d events", len(events))
	}
}

func TestProcessApp_MinLiquidBalanceReserve(t *testing.T) {
	// App has 100 POKT staked, 0 liquid. Target is 200 POKT.
	// MinLiquidBalance = 5 POKT. Fund should cover amountNeeded + txFee + reserve.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Stake:         100 * uPOKT,
			LiquidBalance: 0,
		},
		balance: 200 * uPOKT,
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

	// Upstake amount should NOT include the reserve — only the stake increase
	if executor.upstakeCalls[0].Amount != 100*uPOKT {
		t.Errorf("upstake amount = %d, want %d (reserve should not be staked)", executor.upstakeCalls[0].Amount, 100*uPOKT)
	}
}

func TestProcessApp_ExistingLiquidCoversReserve(t *testing.T) {
	// App has 100 POKT staked, 110 POKT liquid. Target is 200 POKT.
	// MinLiquidBalance = 5 POKT.
	// totalLiquidNeeded = 100 POKT + 1 + 5 POKT = 105_000_001
	// App already has 110 POKT liquid, which is enough. No fund needed.
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Stake:         100 * uPOKT,
			LiquidBalance: 110 * uPOKT,
		},
		balance: 110 * uPOKT,
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

	// No fund needed — app has more than enough liquid
	if len(executor.fundCalls) != 0 {
		t.Errorf("expected 0 fund calls, got %d (amount: %d)", len(executor.fundCalls), executor.fundCalls[0].Amount)
	}
	// Upstake should still happen
	if len(executor.upstakeCalls) != 1 {
		t.Fatalf("expected 1 upstake call, got %d", len(executor.upstakeCalls))
	}
	if executor.upstakeCalls[0].Amount != 100*uPOKT {
		t.Errorf("upstake amount = %d, want %d", executor.upstakeCalls[0].Amount, 100*uPOKT)
	}
}

func TestProcessApp_PartialLiquidBalance(t *testing.T) {
	// App has 100 POKT staked, 50 POKT liquid. Target is 200 POKT.
	// MinLiquidBalance = 5 POKT.
	// totalLiquidNeeded = 100 POKT + 1 + 5 POKT = 105_000_001
	// Fund = 105_000_001 - 50_000_000 = 55_000_001
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Stake:         100 * uPOKT,
			LiquidBalance: 50 * uPOKT,
		},
		balance: 200 * uPOKT,
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
}

func TestProcessApp_StakeAboveThreshold_Skips(t *testing.T) {
	client := &mockClient{
		app: &models.Application{
			Address:   testAddr,
			ServiceID: "svc1",
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

func TestProcessApp_FundFails_NoUpstake(t *testing.T) {
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
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

func TestProcessApp_ZeroMinLiquid_BackwardCompat(t *testing.T) {
	// With MinLiquidBalance=0 (default), behavior matches the original fix:
	// fund = amountNeeded + txFee - liquid
	client := &mockClient{
		app: &models.Application{
			Address:       testAddr,
			ServiceID:     "svc1",
			Stake:         180 * uPOKT,
			LiquidBalance: 10 * uPOKT,
		},
		balance: 200 * uPOKT,
	}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfg := models.AutoTopUpConfig{
		Enabled:          true,
		TriggerThreshold: 190 * uPOKT,
		TargetAmount:     200 * uPOKT,
		MinLiquidBalance: 0, // default
	}
	netCfg := w.Config.Config.Networks[testNetwork]

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg)

	// amountNeeded = 20 POKT, liquid = 10 POKT
	// totalLiquidNeeded = 20 POKT + 1 + 0 = 20_000_001
	// fund = 20_000_001 - 10_000_000 = 10_000_001
	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(20*uPOKT + 1 - 10*uPOKT)
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}
}
