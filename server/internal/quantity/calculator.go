package quantity

import (
	"fmt"
	"log/slog"
	"math"

	"github.com/botbtc/server/internal/config"
)

// QuantityResult holds the result of a quantity calculation.
type QuantityResult struct {
	RiskAmountUSD     float64 `json:"risk_amount_usd"`
	StopDistanceUSD   float64 `json:"stop_distance_usd"`
	RawQtyBTC         float64 `json:"raw_qty_btc"`
	NormalizedQtyBTC  float64 `json:"normalized_qty_btc"`
	NotionalUSD       float64 `json:"notional_usd"`
	MarginRequiredUSD float64 `json:"margin_required_usd"`
	LeverageRequired  float64 `json:"leverage_required"`
	Valid             bool    `json:"valid"`
	RejectReason      string  `json:"reject_reason,omitempty"`
}

// Calculator computes and normalizes BTC quantity for BingX orders.
// The quantity is always calculated server-side, never by MT5.
type Calculator struct {
	cfg    config.QuantityConfig
	logger *slog.Logger
}

// NewCalculator creates a new quantity calculator.
func NewCalculator(cfg config.QuantityConfig, logger *slog.Logger) *Calculator {
	return &Calculator{
		cfg:    cfg,
		logger: logger,
	}
}

// Calculate computes the order quantity based on risk parameters.
//
//	equityUSD:      current account equity
//	riskPercent:    risk as percentage (e.g. 1.0 = 1%)
//	stopDistanceUSD: stop loss distance in USD
//	btcPrice:       current BTC price
//	leverage:       leverage to use
func (c *Calculator) Calculate(equityUSD, riskPercent, stopDistanceUSD, btcPrice float64, leverage int) QuantityResult {
	res := QuantityResult{
		StopDistanceUSD: stopDistanceUSD,
	}

	// --- Sanity checks ---
	if stopDistanceUSD <= 0 {
		res.RejectReason = "stop_distance_usd must be positive"
		return res
	}
	if btcPrice <= 0 {
		res.RejectReason = "btc_price must be positive"
		return res
	}
	if equityUSD <= 0 {
		res.RejectReason = "equity_usd must be positive"
		return res
	}
	if leverage <= 0 {
		res.RejectReason = "leverage must be positive"
		return res
	}

	// --- Risk amount ---
	res.RiskAmountUSD = equityUSD * riskPercent / 100.0

	// --- Raw quantity: risk / stop distance gives BTC qty ---
	res.RawQtyBTC = res.RiskAmountUSD / stopDistanceUSD

	// --- Normalize to exchange step ---
	res.NormalizedQtyBTC = c.NormalizeQuantity(res.RawQtyBTC)

	// --- Validate min qty ---
	if res.NormalizedQtyBTC < c.cfg.MinQty {
		res.RejectReason = fmt.Sprintf(
			"normalized qty %.6f < min_qty %.6f (risk $%.2f too small for stop $%.2f)",
			res.NormalizedQtyBTC, c.cfg.MinQty, res.RiskAmountUSD, stopDistanceUSD,
		)
		return res
	}

	// --- Validate max qty ---
	if c.cfg.MaxQty > 0 && res.NormalizedQtyBTC > c.cfg.MaxQty {
		res.NormalizedQtyBTC = c.NormalizeQuantity(c.cfg.MaxQty)
		c.logger.Warn("quantity: clamped to max_qty",
			"raw", res.RawQtyBTC,
			"clamped", res.NormalizedQtyBTC,
		)
	}

	// --- Notional and margin ---
	res.NotionalUSD = res.NormalizedQtyBTC * btcPrice
	res.MarginRequiredUSD = res.NotionalUSD / float64(leverage)
	res.LeverageRequired = float64(leverage)

	// --- Validate leverage ---
	if c.cfg.MaxLeverage > 0 && leverage > c.cfg.MaxLeverage {
		res.RejectReason = fmt.Sprintf(
			"leverage %d exceeds max_leverage %d",
			leverage, c.cfg.MaxLeverage,
		)
		return res
	}

	// --- Validate margin ---
	if res.MarginRequiredUSD > equityUSD {
		res.RejectReason = fmt.Sprintf(
			"margin_required $%.2f exceeds equity $%.2f",
			res.MarginRequiredUSD, equityUSD,
		)
		return res
	}

	res.Valid = true
	c.logger.Info("quantity: calculated",
		"risk_usd", res.RiskAmountUSD,
		"raw_qty", res.RawQtyBTC,
		"normalized_qty", res.NormalizedQtyBTC,
		"notional_usd", res.NotionalUSD,
		"margin_usd", res.MarginRequiredUSD,
	)

	return res
}

// NormalizeQuantity rounds qty down to the nearest valid step.
func (c *Calculator) NormalizeQuantity(qty float64) float64 {
	if c.cfg.QtyStep <= 0 {
		return qty
	}
	stepped := math.Floor(qty/c.cfg.QtyStep) * c.cfg.QtyStep
	// Round to precision to avoid floating-point drift.
	return roundTo(stepped, c.cfg.QtyPrecision)
}

// NormalizePrice rounds price to the correct precision.
func (c *Calculator) NormalizePrice(price float64) float64 {
	return roundTo(price, c.cfg.PricePrecision)
}

// roundTo rounds v to n decimal places.
func roundTo(v float64, n int) float64 {
	pow := math.Pow10(n)
	return math.Round(v*pow) / pow
}
