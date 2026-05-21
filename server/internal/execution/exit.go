package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/botbtc/server/internal/bingx"
)

// monitorPosition watches the active position and executes exits.
// It runs in a goroutine, checking exit conditions every ~50ms.
//
// Exit conditions:
//  1. Stop Loss: price moved against us beyond InitialStopLossUSD
//  2. Take Profit: price moved in our favor beyond MaxTakeProfitUSD
//  3. Trailing Stop: if enabled, lock profit at trailing steps
//  4. Max Duration: exceeded MaxTradeDurationMs
//  5. Time Stop (No Progress): price hasn't moved MinFavorableMoveUSD in TimeStopNoProgressMs
//  6. Kill switch activated
func (p *Pipeline) monitorPosition(ctx context.Context) {
	p.logger.Info("exit: position monitor started")
	defer p.logger.Info("exit: position monitor stopped")

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// Track progress for time-stop.
	lastProgressTime := time.Now()
	bestFavorable := 0.0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		p.mu.Lock()
		pos := p.activePos
		p.mu.Unlock()

		if pos == nil {
			return
		}

		// Get latest market data.
		book := p.marketPoller.LatestBook()
		if book == nil {
			continue
		}

		// Calculate current PnL based on position side.
		var currentPnL float64
		var currentPrice float64
		if pos.Side == "BUY" {
			currentPrice = book.BidPrice // we'd exit at bid
			currentPnL = (currentPrice - pos.EntryPrice) * pos.Quantity
		} else {
			currentPrice = book.AskPrice // we'd exit at ask
			currentPnL = (pos.EntryPrice - currentPrice) * pos.Quantity
		}

		// Track best PnL for trailing.
		p.mu.Lock()
		if currentPnL > p.activePos.BestPnL {
			p.activePos.BestPnL = currentPnL
		}
		p.mu.Unlock()

		elapsed := time.Since(pos.EntryTime)

		// ─── Check 6: Kill switch ─────────────────────────────────
		if p.riskEngine.IsKillSwitchActive() {
			if err := p.executeExit(ctx, "kill_switch"); err != nil {
				p.logger.Error("exit: kill switch exit failed", "error", err)
			}
			return
		}

		// ─── Check 1: Stop Loss ───────────────────────────────────
		if pos.Side == "BUY" && currentPrice <= pos.StopLoss {
			if err := p.executeExit(ctx, fmt.Sprintf("stop_loss (price=%.2f <= sl=%.2f)", currentPrice, pos.StopLoss)); err != nil {
				p.logger.Error("exit: stop loss exit failed", "error", err)
			}
			return
		}
		if pos.Side == "SELL" && currentPrice >= pos.StopLoss {
			if err := p.executeExit(ctx, fmt.Sprintf("stop_loss (price=%.2f >= sl=%.2f)", currentPrice, pos.StopLoss)); err != nil {
				p.logger.Error("exit: stop loss exit failed", "error", err)
			}
			return
		}

		// ─── Check 2: Take Profit ─────────────────────────────────
		if pos.Side == "BUY" && currentPrice >= pos.TakeProfit {
			if err := p.executeExit(ctx, fmt.Sprintf("take_profit (price=%.2f >= tp=%.2f)", currentPrice, pos.TakeProfit)); err != nil {
				p.logger.Error("exit: take profit exit failed", "error", err)
			}
			return
		}
		if pos.Side == "SELL" && currentPrice <= pos.TakeProfit {
			if err := p.executeExit(ctx, fmt.Sprintf("take_profit (price=%.2f <= tp=%.2f)", currentPrice, pos.TakeProfit)); err != nil {
				p.logger.Error("exit: take profit exit failed", "error", err)
			}
			return
		}

		// ─── Check 3: Trailing Stop ───────────────────────────────
		if p.cfg.Trailing.Enabled {
			shouldExit, reason := p.checkTrailingStop(currentPrice)
			if shouldExit {
				if err := p.executeExit(ctx, reason); err != nil {
					p.logger.Error("exit: trailing stop exit failed", "error", err)
				}
				return
			}
		}

		// ─── Check 4: Max Duration ────────────────────────────────
		maxDuration := time.Duration(p.cfg.Trade.MaxTradeDurationMs) * time.Millisecond
		if elapsed >= maxDuration {
			if err := p.executeExit(ctx, fmt.Sprintf("max_duration (%.1fs)", elapsed.Seconds())); err != nil {
				p.logger.Error("exit: max duration exit failed", "error", err)
			}
			return
		}

		// ─── Check 5: Time Stop (No Progress) ────────────────────
		favorableMove := currentPnL / pos.Quantity // per-BTC favorable move in USD
		if favorableMove > bestFavorable+p.cfg.Trade.MinFavorableMoveUSD {
			bestFavorable = favorableMove
			lastProgressTime = time.Now()
		}
		noProgressTimeout := time.Duration(p.cfg.Trade.TimeStopNoProgressMs) * time.Millisecond
		if noProgressTimeout > 0 && time.Since(lastProgressTime) >= noProgressTimeout {
			if err := p.executeExit(ctx, fmt.Sprintf("time_stop_no_progress (%.1fs without $%.2f move)",
				time.Since(lastProgressTime).Seconds(), p.cfg.Trade.MinFavorableMoveUSD)); err != nil {
				p.logger.Error("exit: time stop exit failed", "error", err)
			}
			return
		}
	}
}

// checkTrailingStop evaluates the trailing stop logic against current price.
// Returns (shouldExit, reason) if the position should be closed.
func (p *Pipeline) checkTrailingStop(currentPrice float64) (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pos := p.activePos
	if pos == nil {
		return false, ""
	}

	steps := p.cfg.Trailing.Steps
	if len(steps) == 0 {
		return false, ""
	}

	// Calculate current PnL in USD.
	var currentPnL float64
	if pos.Side == "BUY" {
		currentPnL = (currentPrice - pos.EntryPrice) * pos.Quantity
	} else {
		currentPnL = (pos.EntryPrice - currentPrice) * pos.Quantity
	}

	// Update trailing step: advance to the highest step we've reached.
	for i := pos.TrailingStep; i < len(steps); i++ {
		if pos.BestPnL >= steps[i].ProfitUSD {
			pos.TrailingStep = i + 1
			pos.LockedPnL = steps[i].LockUSD
			p.logger.Info("exit: trailing step advanced",
				"signal_id", pos.SignalID,
				"step", i+1,
				"best_pnl", pos.BestPnL,
				"locked_pnl", pos.LockedPnL,
			)
		} else {
			break
		}
	}

	// If we have a locked PnL and current PnL drops below it, exit.
	if pos.LockedPnL > 0 && currentPnL <= pos.LockedPnL {
		return true, fmt.Sprintf("trailing_stop (pnl=$%.2f <= locked=$%.2f, step=%d)",
			currentPnL, pos.LockedPnL, pos.TrailingStep)
	}

	return false, ""
}

// executeExit closes the active position by placing a reduce-only limit order
// on the opposite side.
func (p *Pipeline) executeExit(ctx context.Context, reason string) error {
	p.mu.Lock()
	pos := p.activePos
	if pos == nil {
		p.mu.Unlock()
		return fmt.Errorf("no active position to exit")
	}
	p.mu.Unlock()

	p.logger.Info("exit: closing position",
		"signal_id", pos.SignalID,
		"side", pos.Side,
		"reason", reason,
	)

	book := p.marketPoller.LatestBook()
	if book == nil {
		return fmt.Errorf("exit: no market data for exit")
	}

	// Determine exit side and price.
	var exitSide bingx.OrderSide
	var exitPrice float64
	if pos.Side == "BUY" {
		exitSide = bingx.SideSell
		exitPrice = book.BidPrice
	} else {
		exitSide = bingx.SideBuy
		exitPrice = book.AskPrice
	}
	exitPrice = p.qtyCalc.NormalizePrice(exitPrice)

	exitClientID := fmt.Sprintf("%s-exit", pos.SignalID)
	if len(exitClientID) > 32 {
		exitClientID = exitClientID[:32]
	}

	// Place reduce-only limit order.
	exitResp, err := p.exchange.PlaceOrder(ctx, bingx.PlaceOrderRequest{
		Symbol:        p.cfg.BingX.Symbol,
		Side:          exitSide,
		PositionSide:  bingx.PositionBoth,
		OrderType:     "LIMIT",
		TimeInForce:   bingx.TIF_IOC,
		Price:         exitPrice,
		Quantity:      pos.Quantity,
		ReduceOnly:    true,
		ClientOrderID: exitClientID,
	})
	if err != nil {
		return fmt.Errorf("exit: place exit order: %w", err)
	}

	p.logger.Info("exit: exit order placed",
		"signal_id", pos.SignalID,
		"exit_order_id", exitResp.OrderID,
		"exit_price", exitPrice,
		"exit_side", exitSide,
	)

	// Wait for fill with retries.
	exitTimeout := 5 * time.Second
	filled, fillInfo, err := p.pollOrderFill(ctx, p.cfg.BingX.Symbol, exitClientID, exitTimeout)
	if err != nil {
		return fmt.Errorf("exit: poll exit order: %w", err)
	}

	// If not filled, try repricing at a more aggressive level.
	if !filled {
		_ = p.cancelOrderSafe(ctx, p.cfg.BingX.Symbol, exitClientID)

		// Reprice with slippage.
		slippage := p.cfg.Orders.MaxExitSlippageUSD
		if pos.Side == "BUY" {
			exitPrice = book.BidPrice - slippage
		} else {
			exitPrice = book.AskPrice + slippage
		}
		exitPrice = p.qtyCalc.NormalizePrice(exitPrice)

		repriceClientID := exitClientID + "-r"
		if len(repriceClientID) > 32 {
			repriceClientID = repriceClientID[:32]
		}

		_, err = p.exchange.PlaceOrder(ctx, bingx.PlaceOrderRequest{
			Symbol:        p.cfg.BingX.Symbol,
			Side:          exitSide,
			PositionSide:  bingx.PositionBoth,
			OrderType:     "LIMIT",
			TimeInForce:   bingx.TIF_IOC,
			Price:         exitPrice,
			Quantity:      pos.Quantity,
			ReduceOnly:    true,
			ClientOrderID: repriceClientID,
		})
		if err != nil {
			p.logger.Error("exit: reprice failed", "error", err)
		} else {
			filled, fillInfo, _ = p.pollOrderFill(ctx, p.cfg.BingX.Symbol, repriceClientID, 3*time.Second)
			if !filled {
				_ = p.cancelOrderSafe(ctx, p.cfg.BingX.Symbol, repriceClientID)
				p.logger.Error("exit: reprice also not filled, position may be stuck",
					"signal_id", pos.SignalID,
				)
			}
		}
	}

	// Record T7 (position closed).
	t7 := time.Now().UnixMilli()
	p.latTracker.RecordClosed(pos.SignalID, t7)

	// Calculate final PnL.
	var realizedPnL float64
	exitFillPrice := exitPrice
	if filled && fillInfo != nil && fillInfo.AvgPrice > 0 {
		exitFillPrice = fillInfo.AvgPrice
	}
	if pos.Side == "BUY" {
		realizedPnL = (exitFillPrice - pos.EntryPrice) * pos.Quantity
	} else {
		realizedPnL = (pos.EntryPrice - exitFillPrice) * pos.Quantity
	}

	p.logger.Info("exit: position closed",
		"signal_id", pos.SignalID,
		"reason", reason,
		"entry_price", pos.EntryPrice,
		"exit_price", exitFillPrice,
		"qty", pos.Quantity,
		"pnl_usd", realizedPnL,
		"duration_ms", time.Since(pos.EntryTime).Milliseconds(),
	)

	// Update risk engine.
	if realizedPnL < 0 {
		p.riskEngine.RecordStopLoss()
	} else {
		p.riskEngine.RecordWin()
	}
	p.riskEngine.SetPositionOpen(false)

	// Clear active position.
	p.mu.Lock()
	p.activePos = nil
	p.mu.Unlock()

	// Cancel monitor context.
	p.mu.Lock()
	if p.monitorCancel != nil {
		p.monitorCancel()
		p.monitorCancel = nil
	}
	p.mu.Unlock()

	return nil
}
