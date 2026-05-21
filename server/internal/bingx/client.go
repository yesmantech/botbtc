package bingx

import (
	"context"
)

// TimeInForce constants for order time-in-force policy.
type TimeInForce string

const (
	TIF_GTC      TimeInForce = "GTC"      // Good-Til-Cancel
	TIF_IOC      TimeInForce = "IOC"      // Immediate-Or-Cancel
	TIF_FOK      TimeInForce = "FOK"      // Fill-Or-Kill
	TIF_PostOnly TimeInForce = "PostOnly" // Maker-only; rejected if would take
)

// OrderSide constants.
type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

// PositionSide for one-way vs hedge mode. Default one-way.
type PositionSide string

const (
	PositionBoth  PositionSide = "BOTH"  // One-way mode
	PositionLong  PositionSide = "LONG"  // Hedge mode
	PositionShort PositionSide = "SHORT" // Hedge mode
)

// PlaceOrderRequest is the input for placing a limit order.
type PlaceOrderRequest struct {
	Symbol        string
	Side          OrderSide
	PositionSide  PositionSide
	OrderType     string // "LIMIT"
	TimeInForce   TimeInForce
	Price         float64
	Quantity      float64
	ReduceOnly    bool
	ClientOrderID string
}

// PlaceOrderResponse is the result from the exchange.
type PlaceOrderResponse struct {
	OrderID       string `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Status        string `json:"status"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
}

// OrderInfo represents full order information from the exchange.
type OrderInfo struct {
	OrderID       string  `json:"orderId"`
	ClientOrderID string  `json:"clientOrderId"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Status        string  `json:"status"`
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
	FilledQty     float64 `json:"executedQty"`
	AvgPrice      float64 `json:"avgPrice"`
	Fee           float64 `json:"fee"`
	FeeAsset      string  `json:"feeAsset"`
	IsMaker       bool    `json:"isMaker"`
	CreateTime    int64   `json:"createTime"`
	UpdateTime    int64   `json:"updateTime"`
}

// PositionInfo represents an open position.
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"positionSide"`
	Quantity         float64 `json:"positionAmt"`
	EntryPrice       float64 `json:"entryPrice"`
	MarkPrice        float64 `json:"markPrice"`
	UnrealizedPnL    float64 `json:"unrealizedProfit"`
	Leverage         int     `json:"leverage"`
	MarginType       string  `json:"marginType"`
	LiquidationPrice float64 `json:"liquidationPrice"`
}

// AccountBalance represents account balance info.
type AccountBalance struct {
	Asset            string  `json:"asset"`
	Balance          float64 `json:"balance"`
	AvailableBalance float64 `json:"availableBalance"`
	CrossUnPnL       float64 `json:"crossUnPnl"`
}

// BookTicker represents best bid/ask from the order book.
type BookTicker struct {
	Symbol    string  `json:"symbol"`
	BidPrice  float64 `json:"bid_price"`
	BidQty    float64 `json:"bid_qty"`
	AskPrice  float64 `json:"ask_price"`
	AskQty    float64 `json:"ask_qty"`
	Timestamp int64   `json:"time"`
}

// ContractInfo represents trading rules for a symbol.
type ContractInfo struct {
	Symbol            string  `json:"symbol"`
	PricePrecision    int     `json:"pricePrecision"`
	QuantityPrecision int     `json:"quantityPrecision"`
	MinQty            float64 `json:"minQty"`
	MaxQty            float64 `json:"maxQty"`
	QtyStep           float64 `json:"stepSize"`
	TickSize          float64 `json:"tickSize"`
	MaxLeverage       int     `json:"maxLeverage"`
}

// ServerTimeResponse for clock sync.
type ServerTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

// Client is the interface for BingX exchange operations.
// This allows swapping between live client and paper/mock implementations.
type Client interface {
	// Market Data
	GetServerTime(ctx context.Context) (int64, error)
	GetContractInfo(ctx context.Context, symbol string) (*ContractInfo, error)
	GetBookTicker(ctx context.Context, symbol string) (*BookTicker, error)

	// Account
	GetBalance(ctx context.Context) ([]AccountBalance, error)
	GetPositions(ctx context.Context, symbol string) ([]PositionInfo, error)
	GetCommissionRate(ctx context.Context, symbol string) (maker, taker float64, err error)

	// Trading
	PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResponse, error)
	CancelOrder(ctx context.Context, symbol, clientOrderID string) error
	GetOrder(ctx context.Context, symbol, clientOrderID string) (*OrderInfo, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error)

	// Connection
	Close() error
}
