package model

import (
	"fmt"
	"time"
)

// Order represents a single order sent to the exchange.
type Order struct {
	ClientOrderID   string     `json:"client_order_id"`
	ExchangeOrderID string     `json:"exchange_order_id"`
	SignalID        string     `json:"signal_id"`
	Symbol          string     `json:"symbol"`
	Side            string     `json:"side"`
	Role            string     `json:"role"` // "entry" or "exit"
	OrderType       string     `json:"order_type"`
	TimeInForce     string     `json:"time_in_force"`
	LimitPrice      float64    `json:"limit_price"`
	Qty             float64    `json:"qty"`
	Status          OrderState `json:"status"`
	AvgFillPrice    float64    `json:"avg_fill_price"`
	FilledQty       float64    `json:"filled_qty"`
	ReduceOnly      bool       `json:"reduce_only"`
	T4SendMs        int64      `json:"t4_send_ms"`
	T5AckMs         int64      `json:"t5_ack_ms"`
	T6FillMs        int64      `json:"t6_fill_ms"`
	CreatedAt       time.Time  `json:"created_at"`
}

// OrderState represents the lifecycle state of an order.
type OrderState string

const (
	OrderNew              OrderState = "NEW"
	OrderSubmitting       OrderState = "SUBMITTING"
	OrderAcked            OrderState = "ACKED"
	OrderPartiallyFilled  OrderState = "PARTIALLY_FILLED"
	OrderFilled           OrderState = "FILLED"
	OrderCancelRequested  OrderState = "CANCEL_REQUESTED"
	OrderCancelled        OrderState = "CANCELLED"
	OrderReplaceRequested OrderState = "REPLACE_REQUESTED"
	OrderReplaced         OrderState = "REPLACED"
	OrderRejected         OrderState = "REJECTED"
	OrderExpired          OrderState = "EXPIRED"
	OrderUnknown          OrderState = "UNKNOWN"
)

// ValidOrderTransitions defines allowed state transitions for orders.
var ValidOrderTransitions = map[OrderState][]OrderState{
	OrderNew:              {OrderSubmitting, OrderRejected},
	OrderSubmitting:       {OrderAcked, OrderRejected, OrderUnknown},
	OrderAcked:            {OrderPartiallyFilled, OrderFilled, OrderCancelRequested, OrderReplaceRequested, OrderExpired, OrderRejected},
	OrderPartiallyFilled:  {OrderFilled, OrderCancelRequested, OrderReplaceRequested, OrderExpired},
	OrderFilled:           {},
	OrderCancelRequested:  {OrderCancelled, OrderFilled, OrderPartiallyFilled, OrderUnknown},
	OrderCancelled:        {},
	OrderReplaceRequested: {OrderReplaced, OrderCancelled, OrderFilled, OrderUnknown},
	OrderReplaced:         {OrderAcked},
	OrderRejected:         {},
	OrderExpired:          {},
	OrderUnknown:          {OrderAcked, OrderFilled, OrderCancelled, OrderRejected, OrderExpired},
}

// CanTransitionTo checks if the order can transition from its current state to the target state.
func (s OrderState) CanTransitionTo(target OrderState) error {
	allowed, ok := ValidOrderTransitions[s]
	if !ok {
		return fmt.Errorf("unknown order state: %s", s)
	}
	for _, a := range allowed {
		if a == target {
			return nil
		}
	}
	return fmt.Errorf("invalid order transition: %s -> %s", s, target)
}
