package risk

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/model"
)

// Engine evaluates signals against risk limits and manages daily risk state.
type Engine struct {
	cfg config.RiskConfig

	mu                 sync.Mutex
	dailyTradeCount    int
	dailyStopLossCount int
	consecutiveLosses  int
	currentRiskPercent float64
	tradingEnabled     bool
	hasOpenPosition    bool
	lastTradeDate      string // "2006-01-02"

	killSwitchActive bool
	killSwitchReason string

	logger *slog.Logger
}

// NewEngine creates a new risk engine with the given configuration.
func NewEngine(cfg config.RiskConfig, logger *slog.Logger) *Engine {
	return &Engine{
		cfg:                cfg,
		currentRiskPercent: cfg.BaseRiskPercent,
		tradingEnabled:     true,
		logger:             logger,
	}
}

// EvaluateSignal checks whether a signal is allowed under current risk limits.
// Returns allowed=true if the signal passes all checks, otherwise returns
// allowed=false with a human-readable reason.
func (e *Engine) EvaluateSignal(_ *model.Signal) (allowed bool, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.maybeResetDaily()

	if e.killSwitchActive {
		return false, fmt.Sprintf("kill switch active: %s", e.killSwitchReason)
	}

	if !e.tradingEnabled {
		return false, "trading disabled (daily stop-loss limit reached)"
	}

	if e.dailyTradeCount >= e.cfg.MaxDailyTrades {
		return false, fmt.Sprintf("daily trade limit reached (%d/%d)", e.dailyTradeCount, e.cfg.MaxDailyTrades)
	}

	if e.hasOpenPosition {
		return false, fmt.Sprintf("max open positions reached (%d/%d)", 1, e.cfg.MaxOpenPositions)
	}

	return true, ""
}

// RecordTrade increments the daily trade counter.
func (e *Engine) RecordTrade() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maybeResetDaily()
	e.dailyTradeCount++
	e.lastTradeDate = today()
	e.logger.Info("risk: trade recorded", "daily_count", e.dailyTradeCount)
}

// RecordStopLoss records a stop-loss event and applies risk escalation logic.
func (e *Engine) RecordStopLoss() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maybeResetDaily()

	e.dailyStopLossCount++
	e.consecutiveLosses++
	e.logger.Info("risk: stop loss recorded",
		"daily_sl_count", e.dailyStopLossCount,
		"consecutive_losses", e.consecutiveLosses,
	)

	// Escalate risk after N consecutive losses.
	if e.consecutiveLosses >= e.cfg.EscalateAfterConsecutiveLosses {
		e.currentRiskPercent = e.cfg.EscalatedRiskPercent
		e.logger.Warn("risk: escalated", "risk_percent", e.currentRiskPercent)
	}

	// Disable trading if daily stop-loss limit is hit.
	if e.dailyStopLossCount >= e.cfg.MaxDailyStopLosses {
		e.tradingEnabled = false
		e.logger.Warn("risk: trading disabled, daily stop-loss limit reached",
			"daily_sl_count", e.dailyStopLossCount,
			"max", e.cfg.MaxDailyStopLosses,
		)
	}
}

// RecordWin resets the consecutive loss counter and returns risk to base level.
func (e *Engine) RecordWin() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consecutiveLosses = 0
	e.currentRiskPercent = e.cfg.BaseRiskPercent
	e.logger.Info("risk: win recorded, risk reset to base", "risk_percent", e.currentRiskPercent)
}

// ResetDaily resets all daily counters. Called automatically on date change
// or explicitly for testing.
func (e *Engine) ResetDaily() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resetDailyLocked()
}

func (e *Engine) resetDailyLocked() {
	e.dailyTradeCount = 0
	e.dailyStopLossCount = 0
	e.consecutiveLosses = 0
	e.currentRiskPercent = e.cfg.BaseRiskPercent
	e.tradingEnabled = true
	e.lastTradeDate = today()
	e.logger.Info("risk: daily counters reset")
}

func (e *Engine) maybeResetDaily() {
	t := today()
	if e.lastTradeDate != "" && e.lastTradeDate != t {
		e.resetDailyLocked()
	}
}

// GetCurrentRisk returns the current risk percentage (1.0 = 1%).
func (e *Engine) GetCurrentRisk() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentRiskPercent
}

// IsKillSwitchActive reports whether the kill switch has been triggered.
func (e *Engine) IsKillSwitchActive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.killSwitchActive
}

// SetKillSwitch activates the kill switch with a reason.
func (e *Engine) SetKillSwitch(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.killSwitchActive = true
	e.killSwitchReason = reason
	e.logger.Error("risk: KILL SWITCH ACTIVATED", "reason", reason)
}

// HasOpenPosition reports whether there is currently an open position.
func (e *Engine) HasOpenPosition() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hasOpenPosition
}

// SetPositionOpen sets the open position flag.
func (e *Engine) SetPositionOpen(open bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hasOpenPosition = open
}

func today() string {
	return time.Now().UTC().Format("2006-01-02")
}
