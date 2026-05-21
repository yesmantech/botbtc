package risk

import (
	"log/slog"
	"os"
	"testing"

	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/model"
)

func testConfig() config.RiskConfig {
	return config.RiskConfig{
		BaseRiskPercent:                1.0,
		EscalatedRiskPercent:           2.0,
		EscalateAfterConsecutiveLosses: 2,
		MaxDailyTrades:                 3,
		MaxDailyStopLosses:             3,
		MaxOpenPositions:               1,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func testSignal() *model.Signal {
	return &model.Signal{
		SignalID: "test-001",
		Symbol:  "BTC-USDT",
		Side:    "BUY",
	}
}

func TestBaseRisk(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())
	risk := e.GetCurrentRisk()
	if risk != 1.0 {
		t.Errorf("expected base risk 1.0, got %f", risk)
	}
}

func TestEscalationAfterConsecutiveLosses(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())

	// One loss: still at base.
	e.RecordStopLoss()
	if r := e.GetCurrentRisk(); r != 1.0 {
		t.Errorf("after 1 loss: expected 1.0, got %f", r)
	}

	// Two losses: should escalate.
	e.RecordStopLoss()
	if r := e.GetCurrentRisk(); r != 2.0 {
		t.Errorf("after 2 losses: expected 2.0, got %f", r)
	}
}

func TestEscalationResetsOnWin(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())
	e.RecordStopLoss()
	e.RecordStopLoss()
	if r := e.GetCurrentRisk(); r != 2.0 {
		t.Fatalf("expected escalated risk 2.0, got %f", r)
	}

	e.RecordWin()
	if r := e.GetCurrentRisk(); r != 1.0 {
		t.Errorf("after win: expected 1.0, got %f", r)
	}
}

func TestDailyTradeLimit(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())

	for i := 0; i < 3; i++ {
		allowed, _ := e.EvaluateSignal(testSignal())
		if !allowed {
			t.Fatalf("trade %d should be allowed", i+1)
		}
		e.RecordTrade()
	}

	allowed, reason := e.EvaluateSignal(testSignal())
	if allowed {
		t.Error("4th trade should be rejected (daily limit)")
	}
	if reason == "" {
		t.Error("rejection should include a reason")
	}
}

func TestDailyStopLossLimit(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())

	for i := 0; i < 3; i++ {
		e.RecordStopLoss()
	}

	allowed, reason := e.EvaluateSignal(testSignal())
	if allowed {
		t.Error("signal should be rejected after 3 stop losses")
	}
	if reason == "" {
		t.Error("rejection should include a reason")
	}
}

func TestMaxOpenPositions(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())
	e.SetPositionOpen(true)

	allowed, reason := e.EvaluateSignal(testSignal())
	if allowed {
		t.Error("signal should be rejected when position is open")
	}
	if reason == "" {
		t.Error("rejection should include a reason")
	}

	// Clear position.
	e.SetPositionOpen(false)
	allowed, _ = e.EvaluateSignal(testSignal())
	if !allowed {
		t.Error("signal should be allowed when no open positions")
	}
}

func TestKillSwitch(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())
	e.SetKillSwitch("manual override")

	if !e.IsKillSwitchActive() {
		t.Error("kill switch should be active")
	}

	allowed, reason := e.EvaluateSignal(testSignal())
	if allowed {
		t.Error("signal should be rejected with kill switch active")
	}
	if reason == "" {
		t.Error("rejection should include a reason")
	}
}

func TestDailyReset(t *testing.T) {
	e := NewEngine(testConfig(), testLogger())

	// Exhaust limits.
	for i := 0; i < 3; i++ {
		e.RecordTrade()
		e.RecordStopLoss()
	}

	allowed, _ := e.EvaluateSignal(testSignal())
	if allowed {
		t.Fatal("should be rejected before reset")
	}

	// Force daily reset.
	e.ResetDaily()

	allowed, _ = e.EvaluateSignal(testSignal())
	if !allowed {
		t.Error("should be allowed after daily reset")
	}

	if r := e.GetCurrentRisk(); r != 1.0 {
		t.Errorf("risk should be reset to 1.0, got %f", r)
	}
}
