package model

import (
	"fmt"
	"time"
)

// Position represents an open or historical position on the exchange.
type Position struct {
	PositionID    string        `json:"position_id"`
	SignalID      string        `json:"signal_id"`
	Symbol        string        `json:"symbol"`
	Side          string        `json:"side"`
	EntryPrice    float64       `json:"entry_price"`
	CurrentPrice  float64       `json:"current_price"`
	Qty           float64       `json:"qty"`
	UnrealizedPnL float64       `json:"unrealized_pnl"`
	Status        PositionState `json:"status"`
	EntryTime     time.Time     `json:"entry_time"`
	TrailingStep  int           `json:"trailing_step"`
}

// PositionState represents the lifecycle state of a position.
type PositionState string

const (
	PositionNone      PositionState = "NONE"
	PositionOpening   PositionState = "OPENING"
	PositionOpen      PositionState = "OPEN"
	PositionClosing   PositionState = "CLOSING"
	PositionClosed    PositionState = "CLOSED"
	PositionDesync    PositionState = "DESYNC"
	PositionEmergency PositionState = "EMERGENCY"
)

// ValidPositionTransitions defines allowed state transitions for positions.
var ValidPositionTransitions = map[PositionState][]PositionState{
	PositionNone:      {PositionOpening},
	PositionOpening:   {PositionOpen, PositionClosed, PositionDesync, PositionEmergency},
	PositionOpen:      {PositionClosing, PositionClosed, PositionDesync, PositionEmergency},
	PositionClosing:   {PositionClosed, PositionDesync, PositionEmergency},
	PositionClosed:    {},
	PositionDesync:    {PositionClosing, PositionClosed, PositionEmergency},
	PositionEmergency: {PositionClosed},
}

// CanTransitionTo checks if the position can transition from its current state to the target state.
func (s PositionState) CanTransitionTo(target PositionState) error {
	allowed, ok := ValidPositionTransitions[s]
	if !ok {
		return fmt.Errorf("unknown position state: %s", s)
	}
	for _, a := range allowed {
		if a == target {
			return nil
		}
	}
	return fmt.Errorf("invalid position transition: %s -> %s", s, target)
}
