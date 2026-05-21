package monitoring

import (
	"log/slog"
	"sync"
)

// LatencyRecord holds all timestamps and computed deltas for a single signal's lifecycle.
type LatencyRecord struct {
	SignalID      string `json:"signal_id"`
	ClientOrderID string `json:"client_order_id,omitempty"`

	// Raw timestamps (milliseconds since epoch).
	T0TickMs     int64 `json:"t0_tick_ms"`     // MT5 tick received
	T1SignalMs   int64 `json:"t1_signal_ms"`   // MT5 signal generated
	T2RecvMs     int64 `json:"t2_recv_ms"`     // Server received signal
	T4SendMs     int64 `json:"t4_send_ms"`     // Order sent to exchange
	T5AckMs      int64 `json:"t5_ack_ms"`      // Order acked by exchange
	T6FillMs     int64 `json:"t6_fill_ms"`     // Order filled
	T7ClosedMs   int64 `json:"t7_closed_ms"`   // Position closed

	// Computed deltas (milliseconds).
	MT5ToBridgeMs   int64 `json:"mt5_to_bridge_ms"`
	SignalToAckMs   int64 `json:"signal_to_ack_ms"`
	SignalToFillMs  int64 `json:"signal_to_fill_ms"`
	AckToFillMs     int64 `json:"ack_to_fill_ms"`
	FillToCloseMs   int64 `json:"fill_to_close_ms"`
	TotalLifetimeMs int64 `json:"total_lifetime_ms"`
}

// LatencyTracker records and computes latency metrics for signals and orders.
type LatencyTracker struct {
	mu      sync.RWMutex
	records map[string]*LatencyRecord // keyed by SignalID
	logger  *slog.Logger
}

// NewLatencyTracker creates a new latency tracker.
func NewLatencyTracker(logger *slog.Logger) *LatencyTracker {
	return &LatencyTracker{
		records: make(map[string]*LatencyRecord),
		logger:  logger,
	}
}

// RecordSignalReceived records the initial timestamps when a signal arrives.
func (lt *LatencyTracker) RecordSignalReceived(signalID string, t0, t1, t2 int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	r := lt.getOrCreateLocked(signalID)
	r.T0TickMs = t0
	r.T1SignalMs = t1
	r.T2RecvMs = t2
	lt.computeDeltasLocked(r)

	lt.logger.Debug("latency: signal received",
		"signal_id", signalID,
		"mt5_to_bridge_ms", r.MT5ToBridgeMs,
	)
}

// RecordOrderSent records when an order is dispatched to the exchange.
func (lt *LatencyTracker) RecordOrderSent(signalID, clientOrderID string, t4 int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	r := lt.getOrCreateLocked(signalID)
	r.ClientOrderID = clientOrderID
	r.T4SendMs = t4
	lt.computeDeltasLocked(r)
}

// RecordOrderAcked records when the exchange acknowledges the order.
func (lt *LatencyTracker) RecordOrderAcked(clientOrderID string, t5 int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	r := lt.findByOrderIDLocked(clientOrderID)
	if r == nil {
		lt.logger.Warn("latency: ack for unknown order", "client_order_id", clientOrderID)
		return
	}
	r.T5AckMs = t5
	lt.computeDeltasLocked(r)
}

// RecordFill records when the order is filled.
func (lt *LatencyTracker) RecordFill(clientOrderID string, t6 int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	r := lt.findByOrderIDLocked(clientOrderID)
	if r == nil {
		lt.logger.Warn("latency: fill for unknown order", "client_order_id", clientOrderID)
		return
	}
	r.T6FillMs = t6
	lt.computeDeltasLocked(r)

	lt.logger.Info("latency: order filled",
		"signal_id", r.SignalID,
		"client_order_id", clientOrderID,
		"signal_to_fill_ms", r.SignalToFillMs,
	)
}

// RecordClosed records when the position is closed.
func (lt *LatencyTracker) RecordClosed(signalID string, t7 int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	r := lt.getOrCreateLocked(signalID)
	r.T7ClosedMs = t7
	lt.computeDeltasLocked(r)

	lt.logger.Info("latency: position closed",
		"signal_id", signalID,
		"total_lifetime_ms", r.TotalLifetimeMs,
	)
}

// GetRecord returns the latency record for a given signal, or nil if not found.
func (lt *LatencyTracker) GetRecord(signalID string) *LatencyRecord {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	r, ok := lt.records[signalID]
	if !ok {
		return nil
	}
	// Return a copy to avoid races.
	cp := *r
	return &cp
}

func (lt *LatencyTracker) getOrCreateLocked(signalID string) *LatencyRecord {
	r, ok := lt.records[signalID]
	if !ok {
		r = &LatencyRecord{SignalID: signalID}
		lt.records[signalID] = r
	}
	return r
}

func (lt *LatencyTracker) findByOrderIDLocked(clientOrderID string) *LatencyRecord {
	for _, r := range lt.records {
		if r.ClientOrderID == clientOrderID {
			return r
		}
	}
	return nil
}

func (lt *LatencyTracker) computeDeltasLocked(r *LatencyRecord) {
	if r.T1SignalMs > 0 && r.T0TickMs > 0 {
		r.MT5ToBridgeMs = r.T1SignalMs - r.T0TickMs
	}
	if r.T5AckMs > 0 && r.T1SignalMs > 0 {
		r.SignalToAckMs = r.T5AckMs - r.T1SignalMs
	}
	if r.T6FillMs > 0 && r.T1SignalMs > 0 {
		r.SignalToFillMs = r.T6FillMs - r.T1SignalMs
	}
	if r.T6FillMs > 0 && r.T5AckMs > 0 {
		r.AckToFillMs = r.T6FillMs - r.T5AckMs
	}
	if r.T7ClosedMs > 0 && r.T6FillMs > 0 {
		r.FillToCloseMs = r.T7ClosedMs - r.T6FillMs
	}
	if r.T7ClosedMs > 0 && r.T0TickMs > 0 {
		r.TotalLifetimeMs = r.T7ClosedMs - r.T0TickMs
	}
}
