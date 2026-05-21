package model

import "fmt"

// Signal represents a trading signal from MT5.
type Signal struct {
	SignalID   string  `json:"signal_id"`
	StrategyID string  `json:"strategy_id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // "BUY" or "SELL"
	SignalPrice float64 `json:"signal_price"`
	Bid        float64 `json:"bid"`
	Ask        float64 `json:"ask"`
	Spread     float64 `json:"spread"`

	// Features
	Velocity250ms  float64 `json:"velocity_250ms"`
	Velocity500ms  float64 `json:"velocity_500ms"`
	Velocity1000ms float64 `json:"velocity_1000ms"`
	Acceleration   float64 `json:"acceleration"`
	EmaFast        float64 `json:"ema_fast"`
	EmaSlow        float64 `json:"ema_slow"`
	MicroATR1s     float64 `json:"micro_atr_1s"`
	MicroATR2s     float64 `json:"micro_atr_2s"`

	// MT5 context
	LegacyLots   float64 `json:"legacy_lots"`
	ContractSize float64 `json:"contract_size"`

	// Timestamps
	T0TickMs   int64 `json:"t0_tick_ms"`
	T1SignalMs int64 `json:"t1_signal_ms"`
}

// SignalState represents the lifecycle state of a signal.
type SignalState string

const (
	SignalCreated          SignalState = "CREATED"
	SignalSentToBridge     SignalState = "SENT_TO_BRIDGE"
	SignalAckedByBridge    SignalState = "ACKED_BY_BRIDGE"
	SignalReceivedByServer SignalState = "RECEIVED_BY_SERVER"
	SignalAcceptedByRisk   SignalState = "ACCEPTED_BY_RISK"
	SignalRejectedByRisk   SignalState = "REJECTED_BY_RISK"
	SignalSubmittedToBingX SignalState = "SUBMITTED_TO_BINGX"
	SignalOrderAcked       SignalState = "ORDER_ACKED"
	SignalOrderFilled      SignalState = "ORDER_FILLED"
	SignalOrderExpired     SignalState = "ORDER_EXPIRED"
	SignalOrderCancelled   SignalState = "ORDER_CANCELLED"
	SignalPositionOpen     SignalState = "POSITION_OPEN"
	SignalPositionClosing  SignalState = "POSITION_CLOSING"
	SignalPositionClosed   SignalState = "POSITION_CLOSED"
	SignalError            SignalState = "ERROR"
)

// ValidSignalTransitions defines allowed state transitions for signals.
var ValidSignalTransitions = map[SignalState][]SignalState{
	SignalCreated:          {SignalSentToBridge, SignalError},
	SignalSentToBridge:     {SignalAckedByBridge, SignalError},
	SignalAckedByBridge:    {SignalReceivedByServer, SignalError},
	SignalReceivedByServer: {SignalAcceptedByRisk, SignalRejectedByRisk, SignalError},
	SignalAcceptedByRisk:   {SignalSubmittedToBingX, SignalError},
	SignalRejectedByRisk:   {},
	SignalSubmittedToBingX: {SignalOrderAcked, SignalOrderExpired, SignalOrderCancelled, SignalError},
	SignalOrderAcked:       {SignalOrderFilled, SignalOrderExpired, SignalOrderCancelled, SignalError},
	SignalOrderFilled:      {SignalPositionOpen, SignalError},
	SignalOrderExpired:     {},
	SignalOrderCancelled:   {},
	SignalPositionOpen:     {SignalPositionClosing, SignalPositionClosed, SignalError},
	SignalPositionClosing:  {SignalPositionClosed, SignalError},
	SignalPositionClosed:   {},
	SignalError:            {},
}

// CanTransitionTo checks if the signal can transition from its current state to the target state.
func (s SignalState) CanTransitionTo(target SignalState) error {
	allowed, ok := ValidSignalTransitions[s]
	if !ok {
		return fmt.Errorf("unknown signal state: %s", s)
	}
	for _, a := range allowed {
		if a == target {
			return nil
		}
	}
	return fmt.Errorf("invalid signal transition: %s -> %s", s, target)
}
