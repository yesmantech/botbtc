package strategygate

import (
	"log/slog"
	"os"
	"testing"

	"github.com/botbtc/server/internal/bingx"
	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func defaultLatencyConfig() config.LatencyConfig {
	return config.LatencyConfig{
		MaxSignalAgeMs:    500,
		MaxMT5ToBridgeMs:  100,
		MaxSignalToAckMs:  200,
		MaxSignalToFillMs: 1000,
	}
}

func defaultOrdersConfig() config.OrdersConfig {
	return config.OrdersConfig{
		OrderType:              "LIMIT",
		MakerProbeTimeoutMs:    100,
		AggressiveIOCTimeoutMs: 50,
		MaxTotalEntryWindowMs:  300,
		MaxRepriceAttempts:     3,
		MaxEntrySlippageUSD:    5.0,
		MaxExitSlippageUSD:     5.0,
	}
}

func makeSignal(side string, price float64, t1Ms int64) *model.Signal {
	return &model.Signal{
		SignalID:    "test-signal-001",
		StrategyID: "scalper-v1",
		Symbol:     "BTC-USDT",
		Side:       side,
		SignalPrice: price,
		T1SignalMs: t1Ms,
	}
}

func makeBook(bid, ask float64, ts int64) *bingx.BookTicker {
	return &bingx.BookTicker{
		Symbol:    "BTC-USDT",
		BidPrice:  bid,
		BidQty:    1.0,
		AskPrice:  ask,
		AskQty:    1.0,
		Timestamp: ts,
	}
}

func TestValidate_ValidSignal(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	signal := makeSignal("BUY", 50000.0, now-100) // 100ms old
	book := makeBook(49999.0, 50001.0, now-50)     // 50ms old

	res := g.Validate(signal, book, now)

	if !res.Valid {
		t.Fatalf("expected valid, got rejected: %s", res.RejectReason)
	}
	// For BUY, suggested entry should be bid (maker probe).
	if res.SuggestedEntry != 49999.0 {
		t.Fatalf("suggested_entry: got %.2f, want 49999.00", res.SuggestedEntry)
	}
	if res.SignalAgeMs != 100 {
		t.Fatalf("signal_age_ms: got %d, want 100", res.SignalAgeMs)
	}
}

func TestValidate_StaleSignal(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	signal := makeSignal("BUY", 50000.0, now-600) // 600ms > max 500ms
	book := makeBook(49999.0, 50001.0, now-50)

	res := g.Validate(signal, book, now)

	if res.Valid {
		t.Fatal("expected rejection for stale signal")
	}
}

func TestValidate_StaleBookTicker(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	signal := makeSignal("BUY", 50000.0, now-100)
	book := makeBook(49999.0, 50001.0, now-600) // 600ms > max 500ms

	res := g.Validate(signal, book, now)

	if res.Valid {
		t.Fatal("expected rejection for stale book ticker")
	}
}

func TestValidate_PriceDislocation(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	// Signal at 50000, book mid at 50010 → dislocation $10 > max $5
	signal := makeSignal("BUY", 50000.0, now-100)
	book := makeBook(50009.0, 50011.0, now-50)

	res := g.Validate(signal, book, now)

	if res.Valid {
		t.Fatal("expected rejection for price dislocation")
	}
}

func TestValidate_WideSpread(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	// Spread = $15 > max ($5 + $5 = $10)
	signal := makeSignal("BUY", 50007.5, now-100) // mid of bid/ask
	book := makeBook(50000.0, 50015.0, now-50)

	res := g.Validate(signal, book, now)

	if res.Valid {
		t.Fatal("expected rejection for wide spread")
	}
}

func TestValidate_SellSide(t *testing.T) {
	g := NewGate(defaultLatencyConfig(), defaultOrdersConfig(), testLogger())

	now := int64(1000000)
	signal := makeSignal("SELL", 50000.0, now-100)
	book := makeBook(49999.0, 50001.0, now-50)

	res := g.Validate(signal, book, now)

	if !res.Valid {
		t.Fatalf("expected valid SELL, got rejected: %s", res.RejectReason)
	}
	// For SELL, suggested entry should be ask (maker probe).
	if res.SuggestedEntry != 50001.0 {
		t.Fatalf("suggested_entry: got %.2f, want 50001.00", res.SuggestedEntry)
	}
}
