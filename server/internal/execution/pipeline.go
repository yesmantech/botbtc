// ─────────────────────────────────────────────────────────────────────────────
// pipeline.go — The Main Execution Orchestrator
// ─────────────────────────────────────────────────────────────────────────────
//
// THIS IS THE BRAIN OF THE BOT. Every signal from MT5 flows through here.
//
// WHAT IT DOES:
// When MT5 detects a trading opportunity and sends a signal via TCP, this
// pipeline processes it through a series of safety checks and then executes
// the trade on BingX. Think of it like a factory assembly line:
//
//   Signal arrives → Risk Check → Market Data Check → Signal Validation
//     → Position Sizing → Entry Order → Position Monitoring → Exit
//
// WHAT EACH STEP DOES:
//   1. Risk Check:     "Are we allowed to trade right now?" (daily limits, kill switch)
//   2. Market Data:    "Do we have fresh prices from BingX?" (stale data = danger)
//   3. Strategy Gate:  "Is the signal still valid?" (age, spread, price dislocation)
//   4. Quantity Calc:  "How much BTC should we buy/sell?" (based on risk %)
//   5. Entry:          "Place the order" (PostOnly probe → IOC fallback)
//   6. Monitor:        "Watch the position" (stop loss, take profit, trailing stop)
//   7. Exit:           "Close the position" (when exit conditions are met)
//
// SAFETY: Only ONE position can be open at a time. If a signal arrives while
// a position is already open, it's rejected by main.go before reaching here.
// ─────────────────────────────────────────────────────────────────────────────
package execution

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/botbtc/server/internal/bingx"
	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/model"
	"github.com/botbtc/server/internal/monitoring"
	"github.com/botbtc/server/internal/orders"
	"github.com/botbtc/server/internal/quantity"
	"github.com/botbtc/server/internal/risk"
	"github.com/botbtc/server/internal/strategygate"
)

// ActivePosition tracks all state for the currently open position.
// Only ONE position can exist at a time (the bot is single-position).
//
// Fields:
//   - SignalID:     unique ID of the signal that opened this position
//   - Side:         "BUY" (we're long, betting price goes up) or "SELL" (short, betting price goes down)
//   - EntryPrice:   the price at which we entered (filled price)
//   - Quantity:     how much BTC we bought/sold
//   - EntryTime:    when the position was opened (for max duration check)
//   - StopLoss:     price level where we cut losses (ALWAYS set, safety net)
//   - TakeProfit:   price level where we take profit
//   - TrailingStep: which trailing stop step we've reached (0 = none yet)
//   - BestPnL:      the highest profit we've seen (used for trailing stop)
//   - LockedPnL:    minimum profit locked by trailing (if price drops below this, we exit)
//   - ExitOrderID:  the order ID of the exit order (if placed)
type ActivePosition struct {
	SignalID     string
	Side         string
	EntryPrice   float64
	Quantity     float64
	EntryTime    time.Time
	StopLoss     float64
	TakeProfit   float64
	TrailingStep int     // index into trailing config steps (0 = not yet trailing)
	BestPnL      float64 // best observed PnL in USD
	LockedPnL    float64 // minimum PnL locked by trailing
	ExitOrderID  string
}

// Pipeline is the main execution orchestrator. It receives validated signals,
// places entry orders, monitors positions, and manages exits.
//
// It holds references to ALL other components (risk engine, strategy gate,
// exchange client, etc.) and coordinates the full trade lifecycle.
//
// Thread safety: activePos is protected by mu (mutex) because it's accessed
// from both the main goroutine (ProcessSignal) and the monitoring goroutine
// (monitorPosition).
type Pipeline struct {
	cfg          *config.Config             // all configuration
	riskEngine   *risk.Engine               // checks daily limits, kill switch
	stratGate    *strategygate.Gate          // validates signal freshness, spread, dislocation
	qtyCalc      *quantity.Calculator        // computes position size from risk %
	orderMgr     *orders.Manager             // tracks order state (created→submitted→filled)
	latTracker   *monitoring.LatencyTracker   // records T0-T7 timestamps
	exchange     bingx.Client                // BingX API client (live or paper)
	marketPoller *bingx.MarketDataPoller      // provides latest bid/ask prices
	logger       *slog.Logger                // structured JSON logger

	// Active position tracking — protected by mutex because it's accessed
	// from multiple goroutines (main loop + monitor goroutine).
	mu        sync.Mutex
	activePos *ActivePosition

	// Cancellation for position monitoring goroutine.
	// When the position is closed, we cancel this to stop the monitor.
	monitorCancel context.CancelFunc
}

// NewPipeline creates a new execution pipeline with all its dependencies.
// This is called once at startup from main.go.
func NewPipeline(
	cfg *config.Config,
	riskEngine *risk.Engine,
	stratGate *strategygate.Gate,
	qtyCalc *quantity.Calculator,
	orderMgr *orders.Manager,
	latTracker *monitoring.LatencyTracker,
	exchange bingx.Client,
	marketPoller *bingx.MarketDataPoller,
	logger *slog.Logger,
) *Pipeline {
	return &Pipeline{
		cfg:          cfg,
		riskEngine:   riskEngine,
		stratGate:    stratGate,
		qtyCalc:      qtyCalc,
		orderMgr:     orderMgr,
		latTracker:   latTracker,
		exchange:     exchange,
		marketPoller: marketPoller,
		logger:       logger,
	}
}

// Start begins background position monitoring. Must be called after pipeline
// creation and before processing signals.
// Note: The actual monitor goroutine is started on-demand when a position opens,
// not here. This just logs that the pipeline is ready.
func (p *Pipeline) Start(ctx context.Context) {
	p.logger.Info("execution pipeline started")
	_ = ctx // ctx will be used when starting monitor goroutines
}

// ProcessSignal is the main entry point. Called for each signal from the bridge.
//
// THIS IS THE MOST IMPORTANT FUNCTION IN THE ENTIRE BOT.
//
// It runs through the complete pipeline:
//   Step 1: Record timestamps (for latency tracking)
//   Step 2: Risk check (are we allowed to trade?)
//   Step 3: Get market data (latest BingX bid/ask)
//   Step 4: Strategy gate (is the signal still valid?)
//   Step 5: Calculate position size (how much BTC?)
//   Step 6: Execute entry order (place limit order on BingX)
//   Step 7: Start position monitoring (watch for exit conditions)
//
// If ANY step fails, the signal is rejected and we return an error.
// The error is logged by main.go but doesn't crash the server.
func (p *Pipeline) ProcessSignal(ctx context.Context, signal *model.Signal) error {
	// Record T2 = time when this function was called (signal reached the pipeline).
	t2 := time.Now().UnixMilli()

	p.logger.Info("pipeline: processing signal",
		"signal_id", signal.SignalID,
		"side", signal.Side,
		"signal_price", signal.SignalPrice,
	)

	// ──────────────────────────────────────────────────────────────
	// Step 1: Record T2 (signal received by pipeline).
	// T0 = MT5 tick time, T1 = MT5 signal generation time, T2 = now.
	// This lets us measure: how long did it take from MT5 → Go server?
	// ──────────────────────────────────────────────────────────────
	p.latTracker.RecordSignalReceived(signal.SignalID, signal.T0TickMs, signal.T1SignalMs, t2)

	// ──────────────────────────────────────────────────────────────
	// Step 2: Risk engine check.
	// Asks: "Are we allowed to trade right now?"
	// Rejects if: daily trade limit hit, daily SL limit hit, kill switch on,
	//             or a position is already open.
	// ──────────────────────────────────────────────────────────────
	allowed, reason := p.riskEngine.EvaluateSignal(signal)
	if !allowed {
		p.logger.Warn("pipeline: signal rejected by risk",
			"signal_id", signal.SignalID,
			"reason", reason,
		)
		return fmt.Errorf("rejected by risk: %s", reason)
	}
	p.logger.Info("pipeline: signal accepted by risk",
		"signal_id", signal.SignalID,
		"risk_percent", p.riskEngine.GetCurrentRisk(),
	)

	// ──────────────────────────────────────────────────────────────
	// Step 3: Get latest BingX book ticker (best bid and best ask).
	// The "book ticker" gives us the current best prices on BingX.
	// If we don't have market data, we can't trade safely.
	// ──────────────────────────────────────────────────────────────
	book := p.marketPoller.LatestBook()
	if book == nil {
		return fmt.Errorf("pipeline: no market data available")
	}

	// ──────────────────────────────────────────────────────────────
	// Step 4: Strategy gate validation.
	// Checks:
	//   - Signal age: is the signal too old? (MT5 might have sent it 500ms ago)
	//   - Book freshness: is our BingX price data recent?
	//   - Price dislocation: has the price moved too far since MT5 generated the signal?
	//   - Spread: is the bid-ask spread too wide? (high spread = expensive to trade)
	// Also computes the suggested entry price (best bid for BUY, best ask for SELL).
	// ──────────────────────────────────────────────────────────────
	nowMs := time.Now().UnixMilli()
	validation := p.stratGate.Validate(signal, book, nowMs)
	if !validation.Valid {
		p.logger.Warn("pipeline: signal rejected by strategy gate",
			"signal_id", signal.SignalID,
			"reason", validation.RejectReason,
		)
		return fmt.Errorf("rejected by strategy gate: %s", validation.RejectReason)
	}

	// ──────────────────────────────────────────────────────────────
	// Step 5: Calculate position size (how much BTC to buy/sell).
	//
	// The math:
	//   risk_amount_usd = equity * risk_percent / 100
	//   qty_btc = risk_amount_usd / stop_distance_usd
	//   margin_needed = qty_btc * btc_price / leverage
	//
	// Example: equity=$1000, risk=1%, SL=$10, BTC=$50000, leverage=20x
	//   risk_amount = $10, qty = 10/10 = 1.0 BTC
	//   margin = 1.0 * 50000 / 20 = $2500 → TOO MUCH! (exceeds equity)
	//   → quantity calculator will reject or clamp this.
	// ──────────────────────────────────────────────────────────────

	// First, get our current USDT balance from BingX.
	balances, err := p.exchange.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("pipeline: get balance: %w", err)
	}
	equity := 0.0
	for _, b := range balances {
		if b.Asset == "USDT" {
			equity = b.Balance
			break
		}
	}
	if equity <= 0 {
		return fmt.Errorf("pipeline: no USDT equity found")
	}

	riskPct := p.riskEngine.GetCurrentRisk()     // 1.0 or 2.0 (if escalated)
	leverage := p.cfg.Quantity.DefaultLeverage     // e.g. 20x
	if leverage <= 0 {
		leverage = 10 // safety fallback
	}
	btcPrice := (book.BidPrice + book.AskPrice) / 2.0  // mid-price
	stopDistUSD := p.cfg.Trade.InitialStopLossUSD       // e.g. $10

	qtyResult := p.qtyCalc.Calculate(equity, riskPct, stopDistUSD, btcPrice, leverage)
	if !qtyResult.Valid {
		return fmt.Errorf("pipeline: quantity rejected: %s", qtyResult.RejectReason)
	}

	p.logger.Info("pipeline: quantity calculated",
		"signal_id", signal.SignalID,
		"qty_btc", qtyResult.NormalizedQtyBTC,
		"notional_usd", qtyResult.NotionalUSD,
		"margin_usd", qtyResult.MarginRequiredUSD,
	)

	// ──────────────────────────────────────────────────────────────
	// Step 6: Execute entry order.
	// This calls executeEntry() in entry.go which:
	//   Phase 1: Places a PostOnly (maker) order at the best bid/ask
	//   Phase 2: If not filled, cancels and tries an aggressive IOC
	// See entry.go for the full flow.
	// ──────────────────────────────────────────────────────────────
	orderResp, order, err := p.executeEntry(ctx, signal, validation, qtyResult.NormalizedQtyBTC)
	if err != nil {
		return fmt.Errorf("pipeline: entry failed: %w", err)
	}

	// ──────────────────────────────────────────────────────────────
	// Step 7: Entry filled → set up position monitoring.
	// The order was filled! Now we:
	//   - Record the trade in the risk engine (for daily counting)
	//   - Calculate stop-loss and take-profit prices
	//   - Create an ActivePosition to track the open trade
	//   - Start a background goroutine to watch for exit conditions
	// ──────────────────────────────────────────────────────────────
	p.riskEngine.RecordTrade()
	p.riskEngine.SetPositionOpen(true)

	// Use the actual fill price if available, otherwise the suggested entry.
	fillPrice := validation.SuggestedEntry
	if order.AvgFillPrice > 0 {
		fillPrice = order.AvgFillPrice
	}

	// Compute stop-loss and take-profit prices based on the fill price.
	// BUY position: SL is below entry, TP is above entry.
	// SELL position: SL is above entry, TP is below entry.
	var slPrice, tpPrice float64
	if signal.Side == "BUY" {
		slPrice = fillPrice - p.cfg.Trade.InitialStopLossUSD   // e.g. 50000 - 10 = 49990
		tpPrice = fillPrice + p.cfg.Trade.MaxTakeProfitUSD     // e.g. 50000 + 50 = 50050
	} else {
		slPrice = fillPrice + p.cfg.Trade.InitialStopLossUSD   // e.g. 50000 + 10 = 50010
		tpPrice = fillPrice - p.cfg.Trade.MaxTakeProfitUSD     // e.g. 50000 - 50 = 49950
	}

	// Store the active position (protected by mutex for thread safety).
	p.mu.Lock()
	p.activePos = &ActivePosition{
		SignalID:   signal.SignalID,
		Side:       signal.Side,
		EntryPrice: fillPrice,
		Quantity:   qtyResult.NormalizedQtyBTC,
		EntryTime:  time.Now(),
		StopLoss:   slPrice,
		TakeProfit: tpPrice,
	}
	p.mu.Unlock()

	p.logger.Info("pipeline: position opened",
		"signal_id", signal.SignalID,
		"side", signal.Side,
		"entry_price", fillPrice,
		"qty", qtyResult.NormalizedQtyBTC,
		"stop_loss", slPrice,
		"take_profit", tpPrice,
		"exchange_order_id", orderResp.OrderID,
	)

	// Start position monitoring in a background goroutine.
	// This goroutine checks exit conditions every ~50ms (see exit.go).
	// It runs until the position is closed or the context is cancelled.
	monCtx, monCancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.monitorCancel = monCancel
	p.mu.Unlock()
	go p.monitorPosition(monCtx)

	return nil
}

// HasActivePosition returns true if there is currently an open position.
// Called by main.go to reject new signals while a position is open.
func (p *Pipeline) HasActivePosition() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activePos != nil
}
