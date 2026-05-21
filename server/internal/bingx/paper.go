// ─────────────────────────────────────────────────────────────────────────────
// paper.go — Paper Trading Client (simulated orders, REAL market prices)
// ─────────────────────────────────────────────────────────────────────────────
//
// This client simulates order execution WITHOUT touching real money.
// BUT it uses REAL BingX market prices for realistic simulation.
//
// What's real:
//   - Book ticker prices (fetched from BingX public API every poll)
//   - Spread, bid/ask levels
//
// What's simulated:
//   - Order fills (instant fill at best bid/ask)
//   - Balance tracking
//   - Position tracking
//   - Fee calculation
// ─────────────────────────────────────────────────────────────────────────────
package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Compile-time interface check.
var _ Client = (*PaperClient)(nil)

// PaperClient simulates BingX for testing without real money.
// It fetches REAL market prices from BingX public API but simulates fills.
type PaperClient struct {
	mu sync.Mutex

	symbol           string
	simulatedBalance float64
	positions        []PositionInfo
	orders           map[string]*OrderInfo
	orderSeq         int64
	bookTicker       *BookTicker

	// Real price fetching — uses BingX public (unauthenticated) API.
	baseURL    string
	httpClient *http.Client

	makerRate float64
	takerRate float64

	logger *slog.Logger
}

// NewPaperClient creates a paper trading client with the given starting balance.
// It connects to BingX public API for real-time prices.
func NewPaperClient(symbol string, initialBalance float64, logger *slog.Logger) *PaperClient {
	return &PaperClient{
		symbol:           symbol,
		simulatedBalance: initialBalance,
		orders:           make(map[string]*OrderInfo),
		baseURL:          "https://open-api.bingx.com",
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		makerRate:        0.0002,
		takerRate:        0.0005,
		logger:           logger,
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

// GetBookTicker fetches the REAL best bid/ask from BingX public API.
// This is an unauthenticated endpoint — no API key needed.
// This way paper trading uses real market prices, not fake ones.
func (p *PaperClient) GetBookTicker(ctx context.Context, symbol string) (*BookTicker, error) {
	// Fetch real price from BingX public API.
	url := fmt.Sprintf("%s/openApi/swap/v2/quote/bookTicker?symbol=%s", p.baseURL, symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return p.fallbackBookTicker()
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Warn("paper: failed to fetch real price, using cached", "error", err)
		return p.fallbackBookTicker()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return p.fallbackBookTicker()
	}

	// Parse BingX API response envelope.
	var apiResp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || apiResp.Code != 0 {
		return p.fallbackBookTicker()
	}

	var bt BookTicker
	if err := json.Unmarshal(apiResp.Data, &bt); err != nil {
		return p.fallbackBookTicker()
	}

	if bt.Timestamp == 0 {
		bt.Timestamp = time.Now().UnixMilli()
	}

	// Cache the latest real price.
	p.mu.Lock()
	p.bookTicker = &bt
	p.mu.Unlock()

	return &bt, nil
}

// fallbackBookTicker returns the last cached book ticker if the API call fails.
func (p *PaperClient) fallbackBookTicker() (*BookTicker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bookTicker == nil {
		return nil, fmt.Errorf("paper: no book ticker available (API unreachable)")
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

// PlaceOrder simulates order placement. For paper trading, orders auto-fill immediately
// at the REAL market price (best ask for BUY, best bid for SELL).
func (p *PaperClient) PlaceOrder(_ context.Context, req PlaceOrderRequest) (*PlaceOrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.orderSeq++
	exchangeID := fmt.Sprintf("PAPER-%d", p.orderSeq)

	// Use the real market price for fill simulation.
	fillPrice := req.Price
	if p.bookTicker != nil {
		if req.Side == SideBuy {
			fillPrice = p.bookTicker.AskPrice // buy at the ask (worst price for buyer)
		} else {
			fillPrice = p.bookTicker.BidPrice // sell at the bid (worst price for seller)
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
