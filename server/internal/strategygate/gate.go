package strategygate

import (
	"fmt"
	"log/slog"
	"math"

	"github.com/botbtc/server/internal/bingx"
	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/model"
)

// ValidationResult holds the outcome of signal validation.
type ValidationResult struct {
	Valid            bool    `json:"valid"`
	RejectReason     string  `json:"reject_reason,omitempty"`
	BingXBid         float64 `json:"bingx_bid"`
	BingXAsk         float64 `json:"bingx_ask"`
	BingXSpread      float64 `json:"bingx_spread"`
	PriceDislocation float64 `json:"price_dislocation"`
	SignalAgeMs      int64   `json:"signal_age_ms"`
	SuggestedEntry   float64 `json:"suggested_entry"`
}

// Gate validates signals against real-time BingX market data.
// It acts as a server-side confirmation layer before order submission.
type Gate struct {
	cfg       config.LatencyConfig
	ordersCfg config.OrdersConfig
	logger    *slog.Logger
}

// NewGate creates a new strategy gate.
func NewGate(cfg config.LatencyConfig, ordersCfg config.OrdersConfig, logger *slog.Logger) *Gate {
	return &Gate{
		cfg:       cfg,
		ordersCfg: ordersCfg,
		logger:    logger,
	}
}

// Validate checks a signal against live BingX data.
func (g *Gate) Validate(signal *model.Signal, book *bingx.BookTicker, nowMs int64) ValidationResult {
	res := ValidationResult{
		BingXBid:    book.BidPrice,
		BingXAsk:    book.AskPrice,
		BingXSpread: book.AskPrice - book.BidPrice,
	}

	// --- Signal age check ---
	res.SignalAgeMs = nowMs - signal.T1SignalMs
	maxAge := int64(g.cfg.MaxSignalAgeMs)
	if res.SignalAgeMs > maxAge {
		res.RejectReason = fmt.Sprintf(
			"signal too stale: age %dms > max %dms",
			res.SignalAgeMs, maxAge,
		)
		g.logger.Warn("strategygate: stale signal",
			"signal_id", signal.SignalID,
			"age_ms", res.SignalAgeMs,
		)
		return res
	}

	// --- Book ticker freshness check ---
	bookAge := nowMs - book.Timestamp
	// Use same threshold as signal age for book freshness.
	if bookAge > maxAge {
		res.RejectReason = fmt.Sprintf(
			"BingX book stale: age %dms > max %dms",
			bookAge, maxAge,
		)
		g.logger.Warn("strategygate: stale book ticker",
			"signal_id", signal.SignalID,
			"book_age_ms", bookAge,
		)
		return res
	}

	// --- Price dislocation check ---
	bingxMid := (book.BidPrice + book.AskPrice) / 2.0
	res.PriceDislocation = math.Abs(signal.SignalPrice - bingxMid)

	maxSlippage := g.ordersCfg.MaxEntrySlippageUSD
	if res.PriceDislocation > maxSlippage {
		res.RejectReason = fmt.Sprintf(
			"price dislocation $%.2f > max_entry_slippage $%.2f (signal=%.2f, bingx_mid=%.2f)",
			res.PriceDislocation, maxSlippage, signal.SignalPrice, bingxMid,
		)
		g.logger.Warn("strategygate: price dislocation",
			"signal_id", signal.SignalID,
			"dislocation", res.PriceDislocation,
			"signal_price", signal.SignalPrice,
			"bingx_mid", bingxMid,
		)
		return res
	}

	// --- Spread check ---
	// Use MaxEntrySlippageUSD * 2 as spread limit (entry+exit slippage budget).
	maxSpread := g.ordersCfg.MaxEntrySlippageUSD + g.ordersCfg.MaxExitSlippageUSD
	if res.BingXSpread > maxSpread {
		res.RejectReason = fmt.Sprintf(
			"BingX spread $%.2f > max allowed $%.2f",
			res.BingXSpread, maxSpread,
		)
		g.logger.Warn("strategygate: wide spread",
			"signal_id", signal.SignalID,
			"spread", res.BingXSpread,
		)
		return res
	}

	// --- Suggest entry price ---
	switch signal.Side {
	case "BUY":
		// For a buy: try the bid for maker probe, ask for aggressive.
		res.SuggestedEntry = book.BidPrice
	case "SELL":
		// For a sell: try the ask for maker probe, bid for aggressive.
		res.SuggestedEntry = book.AskPrice
	default:
		res.RejectReason = fmt.Sprintf("unknown signal side: %s", signal.Side)
		return res
	}

	res.Valid = true
	g.logger.Info("strategygate: signal validated",
		"signal_id", signal.SignalID,
		"side", signal.Side,
		"suggested_entry", res.SuggestedEntry,
		"dislocation", res.PriceDislocation,
		"spread", res.BingXSpread,
		"age_ms", res.SignalAgeMs,
	)
	return res
}
