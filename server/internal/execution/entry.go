package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/botbtc/server/internal/bingx"
	"github.com/botbtc/server/internal/model"
	"github.com/botbtc/server/internal/strategygate"
)

// executeEntry handles the entry order flow:
//
//  1. Post-only maker probe at best bid (BUY) or best ask (SELL).
//  2. Wait makerProbeTimeoutMs for fill.
//  3. If not filled, cancel and try aggressive limit at ask+slippage (BUY) or
//     bid−slippage (SELL) with IOC time-in-force.
//  4. If still not filled after aggressiveIOCTimeoutMs, abort.
func (p *Pipeline) executeEntry(
	ctx context.Context,
	signal *model.Signal,
	validation strategygate.ValidationResult,
	qty float64,
) (*bingx.PlaceOrderResponse, *model.Order, error) {

	entryStart := time.Now()
	maxTotalMs := int64(p.cfg.Orders.MaxTotalEntryWindowMs)

	// ─── Phase 1: Post-only maker probe ──────────────────────────
	order, err := p.orderMgr.CreateOrder(signal, "entry", validation.SuggestedEntry, qty)
	if err != nil {
		return nil, nil, fmt.Errorf("create entry order: %w", err)
	}

	_ = p.orderMgr.UpdateState(order.ClientOrderID, model.OrderSubmitting)

	t4 := time.Now().UnixMilli()
	p.latTracker.RecordOrderSent(signal.SignalID, order.ClientOrderID, t4)
	order.T4SendMs = t4

	positionSide := bingx.PositionBoth
	orderResp, err := p.exchange.PlaceOrder(ctx, bingx.PlaceOrderRequest{
		Symbol:        signal.Symbol,
		Side:          bingx.OrderSide(signal.Side),
		PositionSide:  positionSide,
		OrderType:     "LIMIT",
		TimeInForce:   bingx.TIF_PostOnly,
		Price:         validation.SuggestedEntry,
		Quantity:      qty,
		ReduceOnly:    false,
		ClientOrderID: order.ClientOrderID,
	})
	if err != nil {
		_ = p.orderMgr.UpdateState(order.ClientOrderID, model.OrderRejected)
		return nil, nil, fmt.Errorf("place maker probe: %w", err)
	}

	t5 := time.Now().UnixMilli()
	p.latTracker.RecordOrderAcked(order.ClientOrderID, t5)
	order.T5AckMs = t5
	order.ExchangeOrderID = orderResp.OrderID
	_ = p.orderMgr.UpdateState(order.ClientOrderID, model.OrderAcked)

	p.logger.Info("entry: maker probe placed",
		"signal_id", signal.SignalID,
		"client_order_id", order.ClientOrderID,
		"exchange_order_id", orderResp.OrderID,
		"price", validation.SuggestedEntry,
	)

	// Poll for fill during maker probe window.
	makerTimeout := time.Duration(p.cfg.Orders.MakerProbeTimeoutMs) * time.Millisecond
	filled, fillInfo, err := p.pollOrderFill(ctx, signal.Symbol, order.ClientOrderID, makerTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("poll maker probe: %w", err)
	}

	if filled {
		t6 := time.Now().UnixMilli()
		p.latTracker.RecordFill(order.ClientOrderID, t6)
		order.T6FillMs = t6
		order.Status = model.OrderFilled
		order.AvgFillPrice = fillInfo.AvgPrice
		order.FilledQty = fillInfo.FilledQty
		_ = p.orderMgr.UpdateState(order.ClientOrderID, model.OrderFilled)

		p.logger.Info("entry: maker probe filled",
			"signal_id", signal.SignalID,
			"avg_price", fillInfo.AvgPrice,
			"filled_qty", fillInfo.FilledQty,
		)
		return orderResp, order, nil
	}

	// ─── Phase 2: Cancel maker probe, try aggressive IOC ─────────

	// Check total time budget.
	elapsedMs := time.Since(entryStart).Milliseconds()
	if elapsedMs >= maxTotalMs {
		_ = p.cancelOrderSafe(ctx, signal.Symbol, order.ClientOrderID)
		return nil, nil, fmt.Errorf("entry window exhausted after maker probe (%dms)", elapsedMs)
	}

	// Cancel the unfilled maker probe.
	_ = p.cancelOrderSafe(ctx, signal.Symbol, order.ClientOrderID)
	_ = p.orderMgr.UpdateState(order.ClientOrderID, model.OrderCancelled)

	p.logger.Info("entry: maker probe timed out, trying aggressive IOC",
		"signal_id", signal.SignalID,
		"elapsed_ms", elapsedMs,
	)

	// Calculate aggressive price (cross the spread).
	book := p.marketPoller.LatestBook()
	if book == nil {
		return nil, nil, fmt.Errorf("no market data for aggressive entry")
	}

	var aggressivePrice float64
	slippage := p.cfg.Orders.MaxEntrySlippageUSD
	if signal.Side == "BUY" {
		aggressivePrice = book.AskPrice + slippage
	} else {
		aggressivePrice = book.BidPrice - slippage
	}
	aggressivePrice = p.qtyCalc.NormalizePrice(aggressivePrice)

	// Create a new IOC order (we reuse signal with different role suffix).
	iocClientID := order.ClientOrderID + "-ioc"

	t4ioc := time.Now().UnixMilli()
	p.latTracker.RecordOrderSent(signal.SignalID, iocClientID, t4ioc)

	iocResp, err := p.exchange.PlaceOrder(ctx, bingx.PlaceOrderRequest{
		Symbol:        signal.Symbol,
		Side:          bingx.OrderSide(signal.Side),
		PositionSide:  positionSide,
		OrderType:     "LIMIT",
		TimeInForce:   bingx.TIF_IOC,
		Price:         aggressivePrice,
		Quantity:      qty,
		ReduceOnly:    false,
		ClientOrderID: iocClientID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("place aggressive IOC: %w", err)
	}

	t5ioc := time.Now().UnixMilli()
	p.latTracker.RecordOrderAcked(iocClientID, t5ioc)

	p.logger.Info("entry: aggressive IOC placed",
		"signal_id", signal.SignalID,
		"client_order_id", iocClientID,
		"price", aggressivePrice,
	)

	// Poll for IOC fill.
	iocTimeout := time.Duration(p.cfg.Orders.AggressiveIOCTimeoutMs) * time.Millisecond
	filled, fillInfo, err = p.pollOrderFill(ctx, signal.Symbol, iocClientID, iocTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("poll aggressive IOC: %w", err)
	}

	if filled {
		t6 := time.Now().UnixMilli()
		p.latTracker.RecordFill(iocClientID, t6)
		order.T6FillMs = t6
		order.Status = model.OrderFilled
		order.AvgFillPrice = fillInfo.AvgPrice
		order.FilledQty = fillInfo.FilledQty
		order.ExchangeOrderID = iocResp.OrderID
		order.ClientOrderID = iocClientID

		p.logger.Info("entry: aggressive IOC filled",
			"signal_id", signal.SignalID,
			"avg_price", fillInfo.AvgPrice,
			"filled_qty", fillInfo.FilledQty,
		)
		return iocResp, order, nil
	}

	// IOC not filled — cancel and abort.
	_ = p.cancelOrderSafe(ctx, signal.Symbol, iocClientID)

	return nil, nil, fmt.Errorf("entry: aggressive IOC expired, no fill")
}

// pollOrderFill polls GetOrder until the order is filled or the timeout expires.
// Returns (true, info, nil) on fill, (false, nil, nil) on timeout.
func (p *Pipeline) pollOrderFill(
	ctx context.Context,
	symbol, clientOrderID string,
	timeout time.Duration,
) (bool, *bingx.OrderInfo, error) {

	deadline := time.Now().Add(timeout)
	pollInterval := 20 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		default:
		}

		info, err := p.exchange.GetOrder(ctx, symbol, clientOrderID)
		if err != nil {
			// Transient errors — retry.
			p.logger.Debug("poll: get order error", "error", err, "client_order_id", clientOrderID)
			time.Sleep(pollInterval)
			continue
		}

		switch info.Status {
		case "FILLED":
			return true, info, nil
		case "CANCELLED", "EXPIRED", "REJECTED":
			return false, nil, nil
		}

		time.Sleep(pollInterval)
	}

	return false, nil, nil
}

// cancelOrderSafe cancels an order, ignoring errors (order may already be filled/cancelled).
func (p *Pipeline) cancelOrderSafe(ctx context.Context, symbol, clientOrderID string) error {
	err := p.exchange.CancelOrder(ctx, symbol, clientOrderID)
	if err != nil {
		p.logger.Debug("cancel order (safe): error ignored",
			"client_order_id", clientOrderID,
			"error", err,
		)
	}
	return err
}
