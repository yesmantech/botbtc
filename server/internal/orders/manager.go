package orders

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/botbtc/server/internal/model"
)

// Manager tracks active orders with idempotency and thread-safe access.
type Manager struct {
	mu     sync.RWMutex
	orders map[string]*model.Order // keyed by ClientOrderID

	// Dedup: signalID+role -> ClientOrderID
	seen map[string]string

	accountID string
	logger    *slog.Logger
}

// NewManager creates a new order manager.
func NewManager(accountID string, logger *slog.Logger) *Manager {
	return &Manager{
		orders:    make(map[string]*model.Order),
		seen:      make(map[string]string),
		accountID: accountID,
		logger:    logger,
	}
}

// CreateOrder creates a new order from a signal. Returns an error if a
// duplicate signal_id + role combination already exists (idempotency guard).
func (m *Manager) CreateOrder(signal *model.Signal, role string, price float64, qty float64) (*model.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dedupKey := signal.SignalID + ":" + role
	if existing, ok := m.seen[dedupKey]; ok {
		return nil, fmt.Errorf("duplicate order for signal %s role %s: already exists as %s",
			signal.SignalID, role, existing)
	}

	attempt := 1
	clientOrderID := GenerateClientOrderID(m.accountID, signal.SignalID, role, attempt)

	order := &model.Order{
		ClientOrderID: clientOrderID,
		SignalID:      signal.SignalID,
		Symbol:        signal.Symbol,
		Side:          signal.Side,
		Role:          role,
		LimitPrice:    price,
		Qty:           qty,
		Status:        model.OrderNew,
		ReduceOnly:    role == "exit",
		CreatedAt:     time.Now(),
	}

	m.orders[clientOrderID] = order
	m.seen[dedupKey] = clientOrderID

	m.logger.Info("order created",
		"client_order_id", clientOrderID,
		"signal_id", signal.SignalID,
		"role", role,
		"price", price,
		"qty", qty,
	)

	return order, nil
}

// GetOrder retrieves an order by its ClientOrderID.
func (m *Manager) GetOrder(clientOrderID string) *model.Order {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.orders[clientOrderID]
}

// UpdateState transitions an order to a new state, validating the transition.
func (m *Manager) UpdateState(clientOrderID string, newState model.OrderState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[clientOrderID]
	if !ok {
		return fmt.Errorf("order not found: %s", clientOrderID)
	}

	if err := order.Status.CanTransitionTo(newState); err != nil {
		return fmt.Errorf("order %s: %w", clientOrderID, err)
	}

	old := order.Status
	order.Status = newState

	m.logger.Info("order state updated",
		"client_order_id", clientOrderID,
		"from", old,
		"to", newState,
	)

	return nil
}

// HasPendingOrders returns true if any orders are in a non-terminal state.
func (m *Manager) HasPendingOrders() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, o := range m.orders {
		switch o.Status {
		case model.OrderFilled, model.OrderCancelled, model.OrderRejected, model.OrderExpired:
			continue
		default:
			return true
		}
	}
	return false
}

// GetActiveEntryOrder returns the first non-terminal entry order, or nil.
func (m *Manager) GetActiveEntryOrder() *model.Order {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, o := range m.orders {
		if o.Role != "entry" {
			continue
		}
		switch o.Status {
		case model.OrderFilled, model.OrderCancelled, model.OrderRejected, model.OrderExpired:
			continue
		default:
			return o
		}
	}
	return nil
}
