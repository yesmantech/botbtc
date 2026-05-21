package bridge

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/botbtc/server/internal/model"
)

// ErrHeartbeat is returned when the message is a heartbeat.
var ErrHeartbeat = errors.New("heartbeat message")

// ACK is the acknowledgement response sent back to the bridge client.
type ACK struct {
	SignalID string `json:"signal_id"`
	Status   string `json:"status"` // "OK" or "ERROR"
	T2RecvMs int64  `json:"t2_recv_ms"`
	Message  string `json:"message,omitempty"`
}

// ParseSignal deserialises a JSON-encoded signal from the bridge.
func ParseSignal(data []byte) (*model.Signal, error) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &base); err == nil && base.Type == "heartbeat" {
		return nil, ErrHeartbeat
	}

	var sig model.Signal
	if err := json.Unmarshal(data, &sig); err != nil {
		return nil, fmt.Errorf("parsing signal JSON: %w", err)
	}

	// Also parse to check if "signal" is present instead of/in addition to "side"
	var customSig struct {
		Signal string `json:"signal"`
		Side   string `json:"side"`
	}
	if err := json.Unmarshal(data, &customSig); err == nil {
		if sig.Side == "" && customSig.Side != "" {
			sig.Side = customSig.Side
		}
		if sig.Side == "" && customSig.Signal != "" {
			switch customSig.Signal {
			case "LONG":
				sig.Side = "BUY"
			case "SHORT":
				sig.Side = "SELL"
			default:
				sig.Side = customSig.Signal
			}
		}
	}

	if sig.SignalID == "" {
		return nil, fmt.Errorf("signal missing required field: signal_id")
	}
	if sig.Side != "BUY" && sig.Side != "SELL" {
		return nil, fmt.Errorf("signal has invalid side: %q (want BUY or SELL)", sig.Side)
	}
	return &sig, nil
}

// BuildACK serialises an ACK response as JSON with a trailing newline.
func BuildACK(signalID string, status string) ([]byte, error) {
	ack := ACK{
		SignalID: signalID,
		Status:   status,
	}
	data, err := json.Marshal(ack)
	if err != nil {
		return nil, fmt.Errorf("marshalling ACK: %w", err)
	}
	// Append newline for newline-delimited protocol.
	data = append(data, '\n')
	return data, nil
}
