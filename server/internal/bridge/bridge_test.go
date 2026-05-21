package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSignal_Valid(t *testing.T) {
	raw := `{
		"signal_id": "sig-001",
		"strategy_id": "strat-alpha",
		"symbol": "BTC-USDT",
		"side": "BUY",
		"signal_price": 67500.50,
		"bid": 67500.00,
		"ask": 67501.00,
		"spread": 1.0,
		"velocity_250ms": 12.5,
		"t0_tick_ms": 1700000000000,
		"t1_signal_ms": 1700000000005
	}`

	sig, err := ParseSignal([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.SignalID != "sig-001" {
		t.Errorf("expected signal_id sig-001, got %s", sig.SignalID)
	}
	if sig.Side != "BUY" {
		t.Errorf("expected side BUY, got %s", sig.Side)
	}
	if sig.SignalPrice != 67500.50 {
		t.Errorf("expected signal_price 67500.50, got %f", sig.SignalPrice)
	}
	if sig.T0TickMs != 1700000000000 {
		t.Errorf("expected t0_tick_ms 1700000000000, got %d", sig.T0TickMs)
	}
}

func TestParseSignal_MissingSignalID(t *testing.T) {
	raw := `{"side": "BUY", "symbol": "BTC-USDT"}`
	_, err := ParseSignal([]byte(raw))
	if err == nil {
		t.Fatal("expected error for missing signal_id")
	}
	if !strings.Contains(err.Error(), "signal_id") {
		t.Errorf("error should mention signal_id: %v", err)
	}
}

func TestParseSignal_InvalidSide(t *testing.T) {
	raw := `{"signal_id": "sig-002", "side": "HOLD"}`
	_, err := ParseSignal([]byte(raw))
	if err == nil {
		t.Fatal("expected error for invalid side")
	}
	if !strings.Contains(err.Error(), "invalid side") {
		t.Errorf("error should mention invalid side: %v", err)
	}
}

func TestParseSignal_SignalMapping(t *testing.T) {
	// Test mapping LONG -> BUY and custom fields
	rawLong := `{
		"signal_id": "sig-003",
		"signal": "LONG",
		"timestamp_ms": 1700000000123,
		"bid": 60000.0,
		"ask": 60002.0,
		"velocity250": 5.4,
		"velocity500": 3.2,
		"velocity1000": 1.1
	}`
	sigLong, err := ParseSignal([]byte(rawLong))
	if err != nil {
		t.Fatalf("unexpected error for LONG signal mapping: %v", err)
	}
	if sigLong.Side != "BUY" {
		t.Errorf("expected mapped side BUY, got %q", sigLong.Side)
	}
	if sigLong.T0TickMs != 1700000000123 {
		t.Errorf("expected T0TickMs 1700000000123, got %d", sigLong.T0TickMs)
	}
	if sigLong.T1SignalMs != 1700000000123 {
		t.Errorf("expected T1SignalMs 1700000000123, got %d", sigLong.T1SignalMs)
	}
	if sigLong.Velocity250ms != 5.4 {
		t.Errorf("expected Velocity250ms 5.4, got %f", sigLong.Velocity250ms)
	}
	if sigLong.Velocity500ms != 3.2 {
		t.Errorf("expected Velocity500ms 3.2, got %f", sigLong.Velocity500ms)
	}
	if sigLong.Velocity1000ms != 1.1 {
		t.Errorf("expected Velocity1000ms 1.1, got %f", sigLong.Velocity1000ms)
	}
	if sigLong.SignalPrice != 60001.0 {
		t.Errorf("expected SignalPrice 60001.0, got %f", sigLong.SignalPrice)
	}

	// Test mapping SHORT -> SELL
	rawShort := `{"signal_id": "sig-004", "signal": "SHORT"}`
	sigShort, err := ParseSignal([]byte(rawShort))
	if err != nil {
		t.Fatalf("unexpected error for SHORT signal mapping: %v", err)
	}
	if sigShort.Side != "SELL" {
		t.Errorf("expected mapped side SELL, got %q", sigShort.Side)
	}
}

func TestParseSignal_InvalidJSON(t *testing.T) {
	_, err := ParseSignal([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuildACK_OK(t *testing.T) {
	data, err := BuildACK("sig-001", "OK")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should end with newline.
	if data[len(data)-1] != '\n' {
		t.Error("ACK should end with newline")
	}

	// Parse without trailing newline.
	var ack ACK
	if err := json.Unmarshal(data[:len(data)-1], &ack); err != nil {
		t.Fatalf("failed to parse ACK: %v", err)
	}
	if ack.SignalID != "sig-001" {
		t.Errorf("expected signal_id sig-001, got %s", ack.SignalID)
	}
	if ack.Status != "OK" {
		t.Errorf("expected status OK, got %s", ack.Status)
	}
}

func TestBuildACK_Error(t *testing.T) {
	data, err := BuildACK("sig-002", "ERROR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ack ACK
	if err := json.Unmarshal(data[:len(data)-1], &ack); err != nil {
		t.Fatalf("failed to parse ACK: %v", err)
	}
	if ack.Status != "ERROR" {
		t.Errorf("expected status ERROR, got %s", ack.Status)
	}
}
