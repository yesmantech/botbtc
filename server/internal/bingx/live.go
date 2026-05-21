package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/botbtc/server/internal/config"
)

// Compile-time interface check.
var _ Client = (*LiveClient)(nil)

// LiveClient is a production REST client for the BingX perpetual swap API.
// It authenticates requests using HMAC-SHA256 and adjusts timestamps via
// a measured clock delta (local − server).
type LiveClient struct {
	baseURL         string
	apiKey          string
	apiSecret       string
	recvWindow      int
	symbol          string
	httpClient      *http.Client
	serverTimeDelta int64 // localMs − serverMs
	logger          *slog.Logger
}

// apiResponse is the generic BingX API JSON envelope.
type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// NewLiveClient creates a new authenticated BingX REST client.
func NewLiveClient(cfg config.BingXConfig, logger *slog.Logger) *LiveClient {
	return &LiveClient{
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		apiSecret:  cfg.APISecret,
		recvWindow: cfg.RecvWindow,
		symbol:     cfg.Symbol,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// ---------------------------------------------------------------------------
// Public (unsigned) endpoints
// ---------------------------------------------------------------------------

// GetServerTime returns the exchange server time in milliseconds.
func (c *LiveClient) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.doPublic(ctx, http.MethodGet, "/openApi/swap/v2/server/time", nil)
	if err != nil {
		return 0, err
	}
	var resp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("bingx: parse server time: %w", err)
	}
	return resp.ServerTime, nil
}

// GetContractInfo returns the trading rules for the given symbol.
func (c *LiveClient) GetContractInfo(ctx context.Context, symbol string) (*ContractInfo, error) {
	params := map[string]string{"symbol": symbol}
	body, err := c.doPublic(ctx, http.MethodGet, "/openApi/swap/v2/quote/contracts", params)
	if err != nil {
		return nil, err
	}

	// BingX may return a single object or an array; handle both.
	var contracts []ContractInfo
	if err := json.Unmarshal(body, &contracts); err != nil {
		// Try single object.
		var single ContractInfo
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			return nil, fmt.Errorf("bingx: parse contracts: %w (alt: %v)", err, err2)
		}
		return &single, nil
	}
	for i := range contracts {
		if contracts[i].Symbol == symbol {
			return &contracts[i], nil
		}
	}
	if len(contracts) > 0 {
		return &contracts[0], nil
	}
	return nil, fmt.Errorf("bingx: contract not found for %s", symbol)
}

// GetBookTicker returns the best bid/ask for the given symbol.
func (c *LiveClient) GetBookTicker(ctx context.Context, symbol string) (*BookTicker, error) {
	params := map[string]string{"symbol": symbol}
	body, err := c.doPublic(ctx, http.MethodGet, "/openApi/swap/v2/quote/bookTicker", params)
	if err != nil {
		return nil, err
	}

	// BingX wraps the book ticker: {"book_ticker": {...}}
	var wrapper struct {
		BookTicker BookTicker `json:"book_ticker"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("bingx: parse book ticker: %w", err)
	}
	bt := wrapper.BookTicker
	if bt.Timestamp == 0 {
		bt.Timestamp = time.Now().UnixMilli()
	}
	return &bt, nil
}

// ---------------------------------------------------------------------------
// Signed endpoints — Account
// ---------------------------------------------------------------------------

// GetBalance returns all asset balances.
func (c *LiveClient) GetBalance(ctx context.Context) ([]AccountBalance, error) {
	body, err := c.doSigned(ctx, http.MethodGet, "/openApi/swap/v2/user/balance", nil)
	if err != nil {
		return nil, err
	}

	// Handle: data may be {"balance": {...}} or a direct object.
	var wrapper struct {
		Balance AccountBalance `json:"balance"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Balance.Asset != "" {
		return []AccountBalance{wrapper.Balance}, nil
	}

	var single AccountBalance
	if err := json.Unmarshal(body, &single); err == nil && single.Asset != "" {
		return []AccountBalance{single}, nil
	}

	var list []AccountBalance
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("bingx: parse balance: %w", err)
	}
	return list, nil
}

// GetPositions returns open positions for the given symbol.
func (c *LiveClient) GetPositions(ctx context.Context, symbol string) ([]PositionInfo, error) {
	params := map[string]string{"symbol": symbol}
	body, err := c.doSigned(ctx, http.MethodGet, "/openApi/swap/v2/user/positions", params)
	if err != nil {
		return nil, err
	}
	var positions []PositionInfo
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, fmt.Errorf("bingx: parse positions: %w", err)
	}
	return positions, nil
}

// GetCommissionRate returns the maker and taker commission rates for the symbol.
func (c *LiveClient) GetCommissionRate(ctx context.Context, symbol string) (maker, taker float64, err error) {
	params := map[string]string{"symbol": symbol}
	body, err := c.doSigned(ctx, http.MethodGet, "/openApi/swap/v2/user/commissionRate", params)
	if err != nil {
		return 0, 0, err
	}
	var rate struct {
		MakerRate string `json:"makerCommissionRate"`
		TakerRate string `json:"takerCommissionRate"`
	}
	if err := json.Unmarshal(body, &rate); err != nil {
		return 0, 0, fmt.Errorf("bingx: parse commission: %w", err)
	}
	maker, _ = strconv.ParseFloat(rate.MakerRate, 64)
	taker, _ = strconv.ParseFloat(rate.TakerRate, 64)
	return maker, taker, nil
}

// ---------------------------------------------------------------------------
// Signed endpoints — Trading
// ---------------------------------------------------------------------------

// PlaceOrder sends a LIMIT order to the exchange.
func (c *LiveClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResponse, error) {
	params := map[string]string{
		"symbol":        req.Symbol,
		"side":          string(req.Side),
		"positionSide":  string(req.PositionSide),
		"type":          "LIMIT",
		"timeInForce":   string(req.TimeInForce),
		"price":         strconv.FormatFloat(req.Price, 'f', -1, 64),
		"quantity":      strconv.FormatFloat(req.Quantity, 'f', -1, 64),
		"clientOrderID": req.ClientOrderID,
	}
	if req.ReduceOnly {
		params["reduceOnly"] = "true"
	}

	body, err := c.doSigned(ctx, http.MethodPost, "/openApi/swap/v2/trade/order", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Order PlaceOrderResponse `json:"order"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Order.OrderID != "" {
		return &resp.Order, nil
	}

	var direct PlaceOrderResponse
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, fmt.Errorf("bingx: parse place order: %w", err)
	}
	return &direct, nil
}

// CancelOrder cancels an open order by clientOrderID.
func (c *LiveClient) CancelOrder(ctx context.Context, symbol, clientOrderID string) error {
	params := map[string]string{
		"symbol":        symbol,
		"clientOrderID": clientOrderID,
	}
	_, err := c.doSigned(ctx, http.MethodDelete, "/openApi/swap/v2/trade/order", params)
	return err
}

// GetOrder retrieves order info by clientOrderID.
func (c *LiveClient) GetOrder(ctx context.Context, symbol, clientOrderID string) (*OrderInfo, error) {
	params := map[string]string{
		"symbol":        symbol,
		"clientOrderID": clientOrderID,
	}
	body, err := c.doSigned(ctx, http.MethodGet, "/openApi/swap/v2/trade/order", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Order OrderInfo `json:"order"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Order.OrderID != "" {
		return &resp.Order, nil
	}

	var direct OrderInfo
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, fmt.Errorf("bingx: parse order: %w", err)
	}
	return &direct, nil
}

// GetOpenOrders returns all open orders for the given symbol.
func (c *LiveClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	params := map[string]string{"symbol": symbol}
	body, err := c.doSigned(ctx, http.MethodGet, "/openApi/swap/v2/trade/openOrders", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Orders []OrderInfo `json:"orders"`
	}
	if err := json.Unmarshal(body, &resp); err == nil {
		return resp.Orders, nil
	}

	var orders []OrderInfo
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("bingx: parse open orders: %w", err)
	}
	return orders, nil
}

// SyncServerTime measures and stores the clock delta between local and server.
// Should be called once at startup before any signed requests.
func (c *LiveClient) SyncServerTime(ctx context.Context) error {
	beforeMs := time.Now().UnixMilli()
	serverMs, err := c.GetServerTime(ctx)
	if err != nil {
		return fmt.Errorf("bingx: sync server time: %w", err)
	}
	afterMs := time.Now().UnixMilli()

	// Estimate the server time at the midpoint of the request.
	roundTripMs := afterMs - beforeMs
	estimatedServerMs := serverMs + roundTripMs/2

	c.serverTimeDelta = afterMs - estimatedServerMs

	c.logger.Info("bingx: server time synced",
		"server_time", serverMs,
		"local_time", afterMs,
		"round_trip_ms", roundTripMs,
		"delta_ms", c.serverTimeDelta,
	)
	return nil
}

// Close is a no-op for the REST client.
func (c *LiveClient) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// Internal HTTP helpers
// ---------------------------------------------------------------------------

// doPublic performs an unsigned public API request.
func (c *LiveClient) doPublic(ctx context.Context, method, path string, params map[string]string) (json.RawMessage, error) {
	url := c.baseURL + path
	if len(params) > 0 {
		url += "?" + BuildQueryString(params)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bingx: build request: %w", err)
	}

	return c.executeRequest(req)
}

// doSigned performs a signed API request with HMAC-SHA256 authentication.
func (c *LiveClient) doSigned(ctx context.Context, method, path string, params map[string]string) (json.RawMessage, error) {
	if params == nil {
		params = make(map[string]string)
	}

	// Step 1: Add timestamp adjusted by server time delta.
	AddTimestamp(params, c.serverTimeDelta)

	// Step 2: Add recv window.
	if c.recvWindow > 0 {
		params["recvWindow"] = strconv.Itoa(c.recvWindow)
	}

	// Step 3: Generate HMAC-SHA256 signature.
	signature := SignRequest(c.apiSecret, params)

	// Step 4: Build URL with params + signature.
	queryStr := BuildQueryString(params) + "&signature=" + signature
	url := c.baseURL + path + "?" + queryStr

	var bodyReader io.Reader
	if method == http.MethodPost {
		// For POST, BingX expects params in the query string, not body.
		bodyReader = nil
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("bingx: build signed request: %w", err)
	}

	// Step 5: Add API key header.
	req.Header.Set("X-BX-APIKEY", c.apiKey)

	return c.executeRequest(req)
}

// executeRequest sends the HTTP request and parses the BingX API envelope.
func (c *LiveClient) executeRequest(req *http.Request) (json.RawMessage, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bingx: http request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bingx: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bingx: http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("bingx: unmarshal envelope: %w (body: %s)", err, string(bodyBytes))
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("bingx: api error code=%d msg=%s (path: %s)", apiResp.Code, apiResp.Msg, req.URL.Path)
	}

	return apiResp.Data, nil
}

// buildFormBody encodes parameters as x-www-form-urlencoded for POST requests.
func buildFormBody(params map[string]string) io.Reader {
	return strings.NewReader(BuildQueryString(params))
}
