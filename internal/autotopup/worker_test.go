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
	app         *models.Application
	err         error
	bankBalance int64
	bankErr     error
	account     *models.AccountInfo
	accountErr  error
}

func (m *mockClient) QueryApplication(address, apiEndpoint, network string) (*models.Application, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.app, nil
}

func (m *mockClient) QueryBalance(address, apiEndpoint string) (int64, error) {
	if m.bankErr != nil {
		return 0, m.bankErr
	}
	return m.bankBalance, nil
}

func (m *mockClient) QueryAccount(address, apiEndpoint string) (*models.AccountInfo, error) {
	if m.accountErr != nil {
		return nil, m.accountErr
	}
	if m.account != nil {
		return m.account, nil
	}
	return &models.AccountInfo{AccountNumber: 1, Sequence: 1}, nil
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
	Sequence   uint64
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

func (m *mockExecutor) FundApplicationWithSequence(appAddress, bankAddress, network string, amount int64, rpcEndpoint string, accountNumber, sequence uint64) (*models.TransactionResponse, uint64, error) {
	m.fundCalls = append(m.fundCalls, fundCall{AppAddress: appAddress, Amount: amount, Sequence: sequence})
	if m.fundErr != nil {
		return nil, sequence, m.fundErr
	}
	return &models.TransactionResponse{TxHash: "fund-hash", Success: true}, sequence, nil
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
	return NewWorker(store, cfg, client, executor, appCache, bankCache, nil, logger)
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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

	if len(executor.fundCalls) != 0 {
		t.Errorf("expected no fund calls when stake above threshold, got %d", len(executor.fundCalls))
	}
	if len(executor.upstakeCalls) != 0 {
		t.Errorf("expected no upstake calls when stake above threshold, got %d", len(executor.upstakeCalls))
	}
}

func TestProcessNetwork_ThreadsSequenceAcrossFundTxs(t *testing.T) {
	// Three apps in one cycle, each needs to be funded from the bank.
	// All three fund txs must carry consecutive sequence numbers so they
	// don't race on the chain's account sequence.
	const (
		addrA = "pokt1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		addrB = "pokt1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		addrC = "pokt1cccccccccccccccccccccccccccccccccccccc"
	)

	apps := map[string]*models.Application{
		addrA: {Address: addrA, ServiceID: "svc1", Status: models.AppStatusStaked, Stake: 100 * uPOKT, LiquidBalance: 0},
		addrB: {Address: addrB, ServiceID: "svc2", Status: models.AppStatusStaked, Stake: 100 * uPOKT, LiquidBalance: 0},
		addrC: {Address: addrC, ServiceID: "svc3", Status: models.AppStatusStaked, Stake: 100 * uPOKT, LiquidBalance: 0},
	}

	client := &multiAppClient{apps: apps, bankBalance: 10_000 * uPOKT, account: &models.AccountInfo{AccountNumber: 42, Sequence: 987}}
	executor := &mockExecutor{}
	w := newTestWorker(t, client, executor)

	cfgs := map[string]models.AutoTopUpConfig{
		addrA: {Enabled: true, TriggerThreshold: 150 * uPOKT, TargetAmount: 200 * uPOKT},
		addrB: {Enabled: true, TriggerThreshold: 150 * uPOKT, TargetAmount: 200 * uPOKT},
		addrC: {Enabled: true, TriggerThreshold: 150 * uPOKT, TargetAmount: 200 * uPOKT},
	}
	netCfg := w.Config.Config.Networks[testNetwork]
	w.processNetwork(context.Background(), testNetwork, cfgs, netCfg)

	if len(executor.fundCalls) != 3 {
		t.Fatalf("expected 3 fund calls, got %d", len(executor.fundCalls))
	}

	// Map order is non-deterministic, but the 3 sequences must form the
	// set {987, 988, 989} with no repeats — the actual property we care
	// about is "no in-cycle collisions".
	seen := make(map[uint64]bool)
	for _, c := range executor.fundCalls {
		if c.Sequence < 987 || c.Sequence > 989 {
			t.Errorf("fund call sequence out of range: got %d, want one of {987,988,989}", c.Sequence)
		}
		if seen[c.Sequence] {
			t.Errorf("sequence %d reused — cycle would race", c.Sequence)
		}
		seen[c.Sequence] = true
	}
}

// multiAppClient is a mockClient variant that returns a different
// Application per address, so a cycle iterating multiple apps doesn't
// collapse them all into one fixture.
type multiAppClient struct {
	apps        map[string]*models.Application
	bankBalance int64
	account     *models.AccountInfo
}

func (m *multiAppClient) QueryApplication(address, apiEndpoint, network string) (*models.Application, error) {
	app, ok := m.apps[address]
	if !ok {
		return &models.Application{Address: address, Status: models.AppStatusNotFound}, nil
	}
	return app, nil
}

func (m *multiAppClient) QueryBalance(address, apiEndpoint string) (int64, error) {
	return m.bankBalance, nil
}

func (m *multiAppClient) QueryAccount(address, apiEndpoint string) (*models.AccountInfo, error) {
	if m.account != nil {
		return m.account, nil
	}
	return &models.AccountInfo{AccountNumber: 1, Sequence: 1}, nil
}

func TestProcessApp_SkipsWhenBankInsufficient(t *testing.T) {
	// App needs ~101 POKT fund (target 200 - current stake 100 + minLiquid 1
	// + fee 1, minus liquidBalance 0 = 101 POKT in uPOKT). Bank only has
	// 50 POKT remaining → worker must skip the fund tx and signal demand
	// via a negative return value.
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

	consumed := w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, 50*uPOKT, nil)

	if consumed >= 0 {
		t.Errorf("expected negative return for insufficient bank, got %d", consumed)
	}
	if len(executor.fundCalls) != 0 {
		t.Errorf("expected no fund tx when bank insufficient, got %d", len(executor.fundCalls))
	}
}

func TestProcessApp_SkipsUnbondingApp(t *testing.T) {
	// App is mid-unbond. An upstake would cancel the unbonding on-chain, so the
	// worker must not attempt fund or upstake: undoing a deliberate unstake is
	// the operator's call, made manually from the UI.
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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

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

	w.processApp(context.Background(), testNetwork, testAddr, cfg, netCfg, -1, nil)

	if len(executor.fundCalls) != 1 {
		t.Fatalf("expected 1 fund call, got %d", len(executor.fundCalls))
	}
	expectedFund := int64(20*uPOKT + 1 + 1*uPOKT - 10*uPOKT) // amountNeeded + txFee + default 1 POKT - liquid
	if executor.fundCalls[0].Amount != expectedFund {
		t.Errorf("fund amount = %d, want %d", executor.fundCalls[0].Amount, expectedFund)
	}
}
