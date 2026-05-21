// ─────────────────────────────────────────────────────────────────────────────
// ws_realtime.go — Real-time BingX WebSocket market data client
// ─────────────────────────────────────────────────────────────────────────────
//
// Connects to BingX WebSocket API for real-time book ticker updates.
// Replaces the REST polling approach (~200ms stale) with WebSocket
// streaming (~2ms latency).
//
// Protocol details (BingX):
//   - URL:       wss://open-api-ws.bingx.com/market
//   - Subscribe: {"id":"1","dataType":"BTC-USDT@bookTicker"}
//   - Data is GZIP compressed
//   - Server sends Ping every 5s, we must reply with Pong
// ─────────────────────────────────────────────────────────────────────────────
package bingx

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSMarketClient streams real-time market data from BingX WebSocket.
type WSMarketClient struct {
	wsURL    string
	symbol   string
	bookCh   chan *BookTicker
	logger   *slog.Logger

	mu         sync.RWMutex
	latestBook *BookTicker
	conn       *websocket.Conn

	reconnectDelay time.Duration
}

// NewWSMarketClient creates a new WebSocket market data client.
func NewWSMarketClient(wsURL string, symbol string, logger *slog.Logger) *WSMarketClient {
	return &WSMarketClient{
		wsURL:          wsURL,
		symbol:         symbol,
		bookCh:         make(chan *BookTicker, 256),
		reconnectDelay: 2 * time.Second,
		logger:         logger,
	}
}

// BookChan returns a channel that receives real-time BookTicker updates.
func (w *WSMarketClient) BookChan() <-chan *BookTicker {
	return w.bookCh
}

// LatestBook returns the most recent book ticker (thread-safe).
func (w *WSMarketClient) LatestBook() *BookTicker {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.latestBook == nil {
		return nil
	}
	cp := *w.latestBook
	return &cp
}

// Start connects to BingX WebSocket and streams data.
// Blocks until ctx is cancelled. Auto-reconnects on disconnect.
func (w *WSMarketClient) Start(ctx context.Context) {
	w.logger.Info("websocket market client starting",
		"url", w.wsURL,
		"symbol", w.symbol,
	)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("websocket market client stopped")
			return
		default:
		}

		err := w.connectAndStream(ctx)
		if err != nil {
			w.logger.Warn("websocket disconnected, reconnecting...",
				"error", err,
				"delay", w.reconnectDelay,
			)
		}

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.reconnectDelay):
		}
	}
}

// connectAndStream handles a single WebSocket session.
func (w *WSMarketClient) connectAndStream(ctx context.Context) error {
	// Connect
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, w.wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.logger.Info("websocket connected", "url", w.wsURL)

	// Subscribe to book ticker
	sub := map[string]string{
		"id":       "btc_book",
		"dataType": w.symbol + "@bookTicker",
	}
	if err := conn.WriteJSON(sub); err != nil {
		return fmt.Errorf("ws subscribe: %w", err)
	}
	w.logger.Info("websocket subscribed", "channel", w.symbol+"@bookTicker")

	// Read loop
	msgCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}

		// Decompress GZIP
		msg, err := decompressGzip(rawMsg)
		if err != nil {
			// Not gzipped, try raw
			msg = rawMsg
		}

		msgCount++
		// Log first 5 messages for debugging format
		if msgCount <= 5 {
			w.logger.Debug("websocket raw message",
				"msg_num", msgCount,
				"data", string(msg),
			)
		}

		// Handle ping/pong
		if handlePingPong(conn, msg) {
			continue
		}

		// Parse book ticker update
		bt, err := parseBookTickerWS(msg)
		if err != nil {
			if msgCount <= 10 {
				w.logger.Debug("websocket parse skip", "error", err, "msg_num", msgCount)
			}
			continue
		}

		// Always use current time for freshness — BingX timestamps can be stale
		bt.Timestamp = time.Now().UnixMilli()

		// Update latest book
		w.mu.Lock()
		w.latestBook = bt
		w.mu.Unlock()

		// Log periodically
		if msgCount <= 5 || msgCount%1000 == 0 {
			w.logger.Info("websocket tick",
				"msg_num", msgCount,
				"bid", bt.BidPrice,
				"ask", bt.AskPrice,
			)
		}

		// Send to channel (non-blocking)
		select {
		case w.bookCh <- bt:
		default:
			// Drop oldest if full
			select {
			case <-w.bookCh:
			default:
			}
			select {
			case w.bookCh <- bt:
			default:
			}
		}
	}
}

// decompressGzip decompresses a GZIP-compressed byte slice.
func decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// handlePingPong checks if the message is a ping and responds with pong.
// BingX sends: "Ping" or {"ping": ...}
// We must reply: "Pong" or {"pong": ...}
func handlePingPong(conn *websocket.Conn, msg []byte) bool {
	s := string(msg)

	// Simple text ping
	if s == "Ping" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("Pong"))
		return true
	}

	// JSON ping: {"ping": timestamp}
	var pingMsg struct {
		Ping int64 `json:"ping"`
	}
	if err := json.Unmarshal(msg, &pingMsg); err == nil && pingMsg.Ping > 0 {
		pong := fmt.Sprintf(`{"pong":%d}`, pingMsg.Ping)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(pong))
		return true
	}

	return false
}

// wsBookTickerData represents the BingX WebSocket book ticker message.
type wsBookTickerData struct {
	DataType string          `json:"dataType"`
	Data     json.RawMessage `json:"data"`
}

// wsBookTicker is the inner data of a book ticker WebSocket message.
type wsBookTicker struct {
	Symbol   string  `json:"s"`
	BidPrice float64 `json:"b,string"`
	BidQty   float64 `json:"B,string"`
	AskPrice float64 `json:"a,string"`
	AskQty   float64 `json:"A,string"`
	Time     int64   `json:"T"`
}

// parseBookTickerWS parses a WebSocket book ticker message.
func parseBookTickerWS(msg []byte) (*BookTicker, error) {
	var wrapper wsBookTickerData
	if err := json.Unmarshal(msg, &wrapper); err != nil {
		return nil, err
	}

	// Check if it's a book ticker message
	if wrapper.DataType == "" || wrapper.Data == nil {
		return nil, fmt.Errorf("not a data message")
	}

	// Try parsing as single object
	var wsBT wsBookTicker
	if err := json.Unmarshal(wrapper.Data, &wsBT); err != nil {
		// Try as array (some endpoints return arrays)
		var arr []wsBookTicker
		if err2 := json.Unmarshal(wrapper.Data, &arr); err2 != nil || len(arr) == 0 {
			return nil, fmt.Errorf("parse ws book ticker: %w", err)
		}
		wsBT = arr[0]
	}

	bt := &BookTicker{
		Symbol:    wsBT.Symbol,
		BidPrice:  wsBT.BidPrice,
		BidQty:    wsBT.BidQty,
		AskPrice:  wsBT.AskPrice,
		AskQty:    wsBT.AskQty,
		Timestamp: wsBT.Time,
	}

	if bt.Symbol == "" {
		bt.Symbol = "BTC-USDT"
	}
	if bt.Timestamp == 0 {
		bt.Timestamp = time.Now().UnixMilli()
	}

	return bt, nil
}
