package monitoring

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

const latencyWindowSize = 1000

// Metrics provides Prometheus-compatible metrics exposition without importing
// any Prometheus library. All fields are thread-safe.
type Metrics struct {
	// Counters (atomic – lock-free).
	SignalsReceived  atomic.Int64
	SignalsAccepted  atomic.Int64
	SignalsRejected  atomic.Int64
	OrdersPlaced     atomic.Int64
	OrdersFilled     atomic.Int64
	OrdersCancelled  atomic.Int64
	OrdersRejected   atomic.Int64
	TradesWon        atomic.Int64
	TradesLost       atomic.Int64
	KillSwitchEvents atomic.Int64

	// Gauges (mutex-protected).
	mu              sync.RWMutex
	CurrentPnLUSD   float64
	TotalPnLUSD     float64
	CurrentRiskPct  float64
	DailyTradeCount int
	OpenPositions   int

	// Latency rolling windows (mutex-protected).
	latMu          sync.Mutex
	signalToAckMs  []int64
	signalToFillMs []int64
	ackIdx         int
	fillIdx        int
}

// NewMetrics returns a fully initialised Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		signalToAckMs:  make([]int64, 0, latencyWindowSize),
		signalToFillMs: make([]int64, 0, latencyWindowSize),
	}
}

// ---------------------------------------------------------------------------
// Latency recorders
// ---------------------------------------------------------------------------

// RecordSignalToAck stores a signal-to-ack latency sample.
func (m *Metrics) RecordSignalToAck(ms int64) {
	m.latMu.Lock()
	defer m.latMu.Unlock()

	if len(m.signalToAckMs) < latencyWindowSize {
		m.signalToAckMs = append(m.signalToAckMs, ms)
	} else {
		m.signalToAckMs[m.ackIdx%latencyWindowSize] = ms
	}
	m.ackIdx++
}

// RecordSignalToFill stores a signal-to-fill latency sample.
func (m *Metrics) RecordSignalToFill(ms int64) {
	m.latMu.Lock()
	defer m.latMu.Unlock()

	if len(m.signalToFillMs) < latencyWindowSize {
		m.signalToFillMs = append(m.signalToFillMs, ms)
	} else {
		m.signalToFillMs[m.fillIdx%latencyWindowSize] = ms
	}
	m.fillIdx++
}

// ---------------------------------------------------------------------------
// Gauge setters
// ---------------------------------------------------------------------------

// SetPnL updates the current and total PnL gauges.
func (m *Metrics) SetPnL(current, total float64) {
	m.mu.Lock()
	m.CurrentPnLUSD = current
	m.TotalPnLUSD = total
	m.mu.Unlock()
}

// SetRisk updates the current risk percentage gauge.
func (m *Metrics) SetRisk(pct float64) {
	m.mu.Lock()
	m.CurrentRiskPct = pct
	m.mu.Unlock()
}

// SetDailyTrades updates the daily trade count gauge.
func (m *Metrics) SetDailyTrades(count int) {
	m.mu.Lock()
	m.DailyTradeCount = count
	m.mu.Unlock()
}

// SetOpenPositions updates the open positions gauge.
func (m *Metrics) SetOpenPositions(count int) {
	m.mu.Lock()
	m.OpenPositions = count
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Prometheus text exposition handler
// ---------------------------------------------------------------------------

// Handler returns an http.Handler that serves metrics in Prometheus text
// exposition format (text/plain; version=0.0.4).
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// --- Counters ---
		writeCounter(w, "btc_scalper_signals_received_total", "Total signals received", m.SignalsReceived.Load())
		writeCounter(w, "btc_scalper_signals_accepted_total", "Total signals accepted by risk", m.SignalsAccepted.Load())
		writeCounter(w, "btc_scalper_signals_rejected_total", "Total signals rejected by risk", m.SignalsRejected.Load())
		writeCounter(w, "btc_scalper_orders_placed_total", "Total orders placed", m.OrdersPlaced.Load())
		writeCounter(w, "btc_scalper_orders_filled_total", "Total orders filled", m.OrdersFilled.Load())
		writeCounter(w, "btc_scalper_orders_cancelled_total", "Total orders cancelled", m.OrdersCancelled.Load())
		writeCounter(w, "btc_scalper_orders_rejected_total", "Total orders rejected", m.OrdersRejected.Load())
		writeCounter(w, "btc_scalper_trades_won_total", "Total winning trades", m.TradesWon.Load())
		writeCounter(w, "btc_scalper_trades_lost_total", "Total losing trades", m.TradesLost.Load())
		writeCounter(w, "btc_scalper_kill_switch_events_total", "Total kill switch activations", m.KillSwitchEvents.Load())

		// --- Gauges ---
		m.mu.RLock()
		currentPnL := m.CurrentPnLUSD
		totalPnL := m.TotalPnLUSD
		risk := m.CurrentRiskPct
		dailyTrades := m.DailyTradeCount
		openPos := m.OpenPositions
		m.mu.RUnlock()

		writeGauge(w, "btc_scalper_current_pnl_usd", "Current position PnL in USD", currentPnL)
		writeGauge(w, "btc_scalper_total_pnl_usd", "Total session PnL in USD", totalPnL)
		writeGauge(w, "btc_scalper_current_risk_pct", "Current risk percentage of capital", risk)
		writeGaugeInt(w, "btc_scalper_daily_trade_count", "Number of trades today", dailyTrades)
		writeGaugeInt(w, "btc_scalper_open_positions", "Number of currently open positions", openPos)

		// --- Latency percentiles ---
		m.latMu.Lock()
		ackSnap := make([]int64, len(m.signalToAckMs))
		copy(ackSnap, m.signalToAckMs)
		fillSnap := make([]int64, len(m.signalToFillMs))
		copy(fillSnap, m.signalToFillMs)
		m.latMu.Unlock()

		writeLatencyPercentiles(w, "btc_scalper_signal_to_ack", "Signal to ACK latency", ackSnap)
		writeLatencyPercentiles(w, "btc_scalper_signal_to_fill", "Signal to fill latency", fillSnap)
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeCounter(w http.ResponseWriter, name, help string, val int64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n\n", name, val)
}

func writeGauge(w http.ResponseWriter, name, help string, val float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %.6f\n\n", name, val)
}

func writeGaugeInt(w http.ResponseWriter, name, help string, val int) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %d\n\n", name, val)
}

func writeLatencyPercentiles(w http.ResponseWriter, prefix, help string, samples []int64) {
	if len(samples) == 0 {
		fmt.Fprintf(w, "# HELP %s_p50_ms %s p50\n", prefix, help)
		fmt.Fprintf(w, "# TYPE %s_p50_ms gauge\n", prefix)
		fmt.Fprintf(w, "%s_p50_ms 0\n", prefix)
		fmt.Fprintf(w, "# HELP %s_p95_ms %s p95\n", prefix, help)
		fmt.Fprintf(w, "# TYPE %s_p95_ms gauge\n", prefix)
		fmt.Fprintf(w, "%s_p95_ms 0\n", prefix)
		fmt.Fprintf(w, "# HELP %s_p99_ms %s p99\n", prefix, help)
		fmt.Fprintf(w, "# TYPE %s_p99_ms gauge\n", prefix)
		fmt.Fprintf(w, "%s_p99_ms 0\n\n", prefix)
		return
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	p50 := percentile(samples, 0.50)
	p95 := percentile(samples, 0.95)
	p99 := percentile(samples, 0.99)

	fmt.Fprintf(w, "# HELP %s_p50_ms %s p50\n", prefix, help)
	fmt.Fprintf(w, "# TYPE %s_p50_ms gauge\n", prefix)
	fmt.Fprintf(w, "%s_p50_ms %d\n", prefix, p50)
	fmt.Fprintf(w, "# HELP %s_p95_ms %s p95\n", prefix, help)
	fmt.Fprintf(w, "# TYPE %s_p95_ms gauge\n", prefix)
	fmt.Fprintf(w, "%s_p95_ms %d\n", prefix, p95)
	fmt.Fprintf(w, "# HELP %s_p99_ms %s p99\n", prefix, help)
	fmt.Fprintf(w, "# TYPE %s_p99_ms gauge\n", prefix)
	fmt.Fprintf(w, "%s_p99_ms %d\n\n", prefix, p99)
}

func percentile(sorted []int64, pct float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(pct * float64(n))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
