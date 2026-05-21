package bingx

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Compile-time interface check.
var _ Client = (*PaperClient)(nil)

// PaperClient simulates BingX for testing without real money.
// It uses live market data prices but simulates fill behaviour.
type PaperClient struct {
	mu sync.Mutex

	symbol           string
	simulatedBalance float64
	positions        []PositionInfo
	orders           map[string]*OrderInfo
	orderSeq         int64
	bookTicker       *BookTicker

	makerRate float64
	takerRate float64

	logger *slog.Logger
}

// NewPaperClient creates a paper trading client with the given starting balance.
func NewPaperClient(symbol string, initialBalance float64, logger *slog.Logger) *PaperClient {
	return &PaperClient{
		symbol:           symbol,
		simulatedBalance: initialBalance,
		orders:           make(map[string]*OrderInfo),
		makerRate:        0.0002,
		takerRate:        0.0005,
		bookTicker: &BookTicker{
			Symbol:   symbol,
			BidPrice: 50000.0,
			AskPrice: 50001.0,
			BidQty:   1.0,
			AskQty:   1.0,
		},
		logger: logger,
	}
}

// SetBookTicker injects a live book ticker for price simulation.
func (p *PaperClient) SetBookTicker(bt *BookTicker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bookTicker = bt
}

// GetServerTime returns the current time in milliseconds.
func (p *PaperClient) GetServerTime(_ context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

// GetContractInfo returns hardcoded BTC-USDT contract info.
func (p *PaperClient) GetContractInfo(_ context.Context, symbol string) (*ContractInfo, error) {
	return &ContractInfo{
		Symbol:            symbol,
		PricePrecision:    2,
		QuantityPrecision: 3,
		MinQty:            0.001,
		MaxQty:            100.0,
		QtyStep:           0.001,
		TickSize:          0.01,
		MaxLeverage:       125,
	}, nil
}

// GetBookTicker returns the currently stored book ticker.
func (p *PaperClient) GetBookTicker(_ context.Context, _ string) (*BookTicker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bookTicker == nil {
		return nil, fmt.Errorf("paper: no book ticker available")
	}
	cp := *p.bookTicker
	cp.Timestamp = time.Now().UnixMilli()
	return &cp, nil
}

// GetBalance returns the simulated balance.
func (p *PaperClient) GetBalance(_ context.Context) ([]AccountBalance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return []AccountBalance{
		{
			Asset:            "USDT",
			Balance:          p.simulatedBalance,
			AvailableBalance: p.simulatedBalance,
		},
	}, nil
}

// GetPositions returns simulated positions.
func (p *PaperClient) GetPositions(_ context.Context, _ string) ([]PositionInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]PositionInfo, len(p.positions))
	copy(result, p.positions)
	return result, nil
}

// GetCommissionRate returns the configured fee rates.
func (p *PaperClient) GetCommissionRate(_ context.Context, _ string) (maker, taker float64, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.makerRate, p.takerRate, nil
}

// PlaceOrder simulates order placement. For paper trading, orders auto-fill immediately.
func (p *PaperClient) PlaceOrder(_ context.Context, req PlaceOrderRequest) (*PlaceOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.orderSeq++
	exchangeID := fmt.Sprintf("PAPER-%d", p.orderSeq)

	fillPrice := req.Price
	if p.bookTicker != nil {
		if req.Side == SideBuy {
			fillPrice = p.bookTicker.AskPrice
		} else {
			fillPrice = p.bookTicker.BidPrice
		}
	}

	now := time.Now().UnixMilli()
	info := &OrderInfo{
		OrderID:       exchangeID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          string(req.Side),
		Status:        "FILLED",
		Price:         req.Price,
		Quantity:      req.Quantity,
		FilledQty:     req.Quantity,
		AvgPrice:      fillPrice,
		Fee:           fillPrice * req.Quantity * p.takerRate,
		FeeAsset:      "USDT",
		IsMaker:       false,
		CreateTime:    now,
		UpdateTime:    now,
	}

	p.orders[req.ClientOrderID] = info

	// Update simulated balance with fee.
	p.simulatedBalance -= info.Fee

	// Update simulated positions.
	if !req.ReduceOnly {
		pos := PositionInfo{
			Symbol:     req.Symbol,
			Side:       string(req.Side),
			Quantity:   req.Quantity,
			EntryPrice: fillPrice,
			MarkPrice:  fillPrice,
			Leverage:   10,
			MarginType: "CROSSED",
		}
		p.positions = append(p.positions, pos)
	} else {
		// Close matching positions.
		p.closePositionsLocked(req.Quantity, fillPrice)
	}

	p.logger.Info("paper: order filled",
		"exchange_id", exchangeID,
		"client_order_id", req.ClientOrderID,
		"side", req.Side,
		"qty", req.Quantity,
		"fill_price", fillPrice,
		"fee", info.Fee,
	)

	return &PlaceOrderResponse{
		OrderID:       exchangeID,
		ClientOrderID: req.ClientOrderID,
		Status:        "FILLED",
		Price:         strconv.FormatFloat(req.Price, 'f', -1, 64),
		Quantity:      strconv.FormatFloat(req.Quantity, 'f', -1, 64),
	}, nil
}

// CancelOrder removes an order from the simulated order map.
func (p *PaperClient) CancelOrder(_ context.Context, _ string, clientOrderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, ok := p.orders[clientOrderID]
	if !ok {
		return fmt.Errorf("paper: order not found: %s", clientOrderID)
	}
	info.Status = "CANCELLED"
	info.UpdateTime = time.Now().UnixMilli()

	p.logger.Info("paper: order cancelled", "client_order_id", clientOrderID)
	return nil
}

// GetOrder looks up an order from the simulated map.
func (p *PaperClient) GetOrder(_ context.Context, _ string, clientOrderID string) (*OrderInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, ok := p.orders[clientOrderID]
	if !ok {
		return nil, fmt.Errorf("paper: order not found: %s", clientOrderID)
	}
	cp := *info
	return &cp, nil
}

// GetOpenOrders returns all non-filled, non-cancelled orders.
func (p *PaperClient) GetOpenOrders(_ context.Context, _ string) ([]OrderInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var open []OrderInfo
	for _, o := range p.orders {
		if o.Status != "FILLED" && o.Status != "CANCELLED" {
			open = append(open, *o)
		}
	}
	return open, nil
}

// Close is a no-op for paper client.
func (p *PaperClient) Close() error {
	return nil
}

func (p *PaperClient) closePositionsLocked(qty, exitPrice float64) {
	remaining := qty
	kept := p.positions[:0]
	for _, pos := range p.positions {
		if remaining <= 0 {
			kept = append(kept, pos)
			continue
		}
		if pos.Quantity <= remaining {
			// Fully close this position.
			pnl := (exitPrice - pos.EntryPrice) * pos.Quantity
			if pos.Side == string(SideSell) {
				pnl = -pnl
			}
			p.simulatedBalance += pnl
			remaining -= pos.Quantity
		} else {
			// Partially close.
			pnl := (exitPrice - pos.EntryPrice) * remaining
			if pos.Side == string(SideSell) {
				pnl = -pnl
			}
			p.simulatedBalance += pnl
			pos.Quantity -= remaining
			remaining = 0
			kept = append(kept, pos)
		}
	}
	p.positions = kept
}
