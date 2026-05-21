package quantity

import (
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/botbtc/server/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func defaultQtyConfig() config.QuantityConfig {
	return config.QuantityConfig{
		MinQty:          0.001,
		MaxQty:          100.0,
		QtyStep:         0.001,
		PricePrecision:  2,
		QtyPrecision:    3,
		TickSize:        0.01,
		MaxLeverage:     20,
		DefaultLeverage: 5,
	}
}

func TestCalculate_Normal(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	// equity=$10000, risk=1%, stop=$10, BTC=$50000, leverage=5
	// risk_amount = 10000 * 1 / 100 = $100
	// raw_qty = 100 / 10 = 10 BTC? That's huge. Let's use realistic stop.
	// Actually: raw_qty = riskAmount / stopDistanceUSD
	// stop=$10 means $10 of BTC price movement
	// So 10 BTC * $10 move = $100 risk. Correct.
	// But 10 BTC at $50000 = $500,000 notional. margin = 500000/5 = $100,000 > equity.
	// Let's use: equity=$10000, risk=1%, stop=$100, BTC=$50000, lev=10
	// risk = $100, raw_qty = 100/100 = 1.0 BTC
	// notional = 1.0 * 50000 = $50000, margin = 50000/10 = $5000 < $10000 ✓
	res := c.Calculate(10000.0, 1.0, 100.0, 50000.0, 10)

	if !res.Valid {
		t.Fatalf("expected valid, got rejected: %s", res.RejectReason)
	}
	if math.Abs(res.RiskAmountUSD-100.0) > 0.01 {
		t.Fatalf("risk_amount: got %.2f, want 100.00", res.RiskAmountUSD)
	}
	if math.Abs(res.NormalizedQtyBTC-1.0) > 0.001 {
		t.Fatalf("normalized_qty: got %.6f, want 1.000", res.NormalizedQtyBTC)
	}
	if math.Abs(res.NotionalUSD-50000.0) > 0.01 {
		t.Fatalf("notional: got %.2f, want 50000.00", res.NotionalUSD)
	}
	if math.Abs(res.MarginRequiredUSD-5000.0) > 0.01 {
		t.Fatalf("margin: got %.2f, want 5000.00", res.MarginRequiredUSD)
	}
}

func TestCalculate_MinQtyRejection(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	// equity=$100, risk=0.1%, stop=$500, BTC=$50000, lev=5
	// risk = 100 * 0.1 / 100 = $0.10
	// raw_qty = 0.10 / 500 = 0.0002 BTC
	// normalized = floor(0.0002/0.001)*0.001 = 0.0 BTC < min 0.001 → reject
	res := c.Calculate(100.0, 0.1, 500.0, 50000.0, 5)

	if res.Valid {
		t.Fatal("expected rejection for qty below min_qty")
	}
	if res.RejectReason == "" {
		t.Fatal("expected non-empty reject reason")
	}
}

func TestCalculate_MaxLeverageRejection(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	// leverage=25 exceeds max of 20
	res := c.Calculate(10000.0, 1.0, 100.0, 50000.0, 25)

	if res.Valid {
		t.Fatal("expected rejection for leverage exceeding max")
	}
}

func TestCalculate_QtyStepNormalization(t *testing.T) {
	cfg := defaultQtyConfig()
	cfg.QtyStep = 0.01
	cfg.QtyPrecision = 2
	c := NewCalculator(cfg, testLogger())

	// raw_qty = some fractional value
	// equity=$10000, risk=1%, stop=$73, BTC=$50000, lev=10
	// risk = $100, raw_qty = 100/73 = 1.369863...
	// normalized = floor(1.369863 / 0.01) * 0.01 = 136 * 0.01 = 1.36
	res := c.Calculate(10000.0, 1.0, 73.0, 50000.0, 10)

	if !res.Valid {
		t.Fatalf("expected valid, got rejected: %s", res.RejectReason)
	}
	if math.Abs(res.NormalizedQtyBTC-1.36) > 0.001 {
		t.Fatalf("normalized_qty: got %.6f, want 1.360", res.NormalizedQtyBTC)
	}
}

func TestCalculate_ZeroStopDistance(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	res := c.Calculate(10000.0, 1.0, 0.0, 50000.0, 5)

	if res.Valid {
		t.Fatal("expected rejection for zero stop distance")
	}
	if res.RejectReason == "" {
		t.Fatal("expected non-empty reject reason")
	}
}

func TestNormalizePrice(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	got := c.NormalizePrice(50123.456789)
	want := 50123.46
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("NormalizePrice: got %.6f, want %.2f", got, want)
	}
}

func TestCalculate_MarginExceedsEquity(t *testing.T) {
	c := NewCalculator(defaultQtyConfig(), testLogger())

	// equity=$1000, risk=10%, stop=$10, BTC=$50000, lev=2
	// risk = $100, raw_qty = 100/10 = 10 BTC
	// notional = 10*50000 = $500,000, margin = 500000/2 = $250,000 > $1000 → reject
	res := c.Calculate(1000.0, 10.0, 10.0, 50000.0, 2)

	if res.Valid {
		t.Fatal("expected rejection for margin exceeding equity")
	}
}
