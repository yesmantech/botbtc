// ─────────────────────────────────────────────────────────────────────────────
// pricefeed.go — TCP server that pushes BingX prices to MT5
// ─────────────────────────────────────────────────────────────────────────────
//
// The Go server already polls BingX for book ticker data every 200ms.
// This module takes those prices and pushes them to connected MT5 clients
// via a simple TCP text protocol: "BID ASK\n"
//
// This avoids MT5 having to make blocking WebRequest() HTTP calls,
// keeping the trading EA completely free of network latency.
// ─────────────────────────────────────────────────────────────────────────────
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// PriceFeedServer pushes market data to MT5 clients via TCP.
type PriceFeedServer struct {
	host     string
	port     int
	listener net.Listener
	logger   *slog.Logger

	mu      sync.Mutex
	clients map[net.Conn]struct{}

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewPriceFeedServer creates a new price feed TCP server.
func NewPriceFeedServer(host string, port int, logger *slog.Logger) *PriceFeedServer {
	return &PriceFeedServer{
		host:    host,
		port:    port,
		clients: make(map[net.Conn]struct{}),
		logger:  logger,
	}
}

// Start begins listening for MT5 price feed connections.
func (s *PriceFeedServer) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("pricefeed listen on %s: %w", addr, err)
	}
	s.listener = ln
	s.logger.Info("price feed server listening", "addr", addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ctx)
	}()

	return nil
}

// Stop shuts down the price feed server.
func (s *PriceFeedServer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Lock()
	for conn := range s.clients {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.logger.Info("price feed server stopped")
}

// BroadcastTick sends a bid/ask update to all connected MT5 clients.
// Format: "BID ASK\n" — simple text protocol, easy to parse in MQL5.
func (s *PriceFeedServer) BroadcastTick(bid, ask float64) {
	msg := fmt.Sprintf("%.1f %.1f\n", bid, ask)
	data := []byte(msg)

	s.mu.Lock()
	defer s.mu.Unlock()

	for conn := range s.clients {
		_, err := conn.Write(data)
		if err != nil {
			s.logger.Debug("price feed: client disconnected", "remote", conn.RemoteAddr())
			_ = conn.Close()
			delete(s.clients, conn)
		}
	}
}

// ClientCount returns the number of connected MT5 clients.
func (s *PriceFeedServer) ClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *PriceFeedServer) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.logger.Error("price feed accept error", "error", err)
			continue
		}

		s.mu.Lock()
		s.clients[conn] = struct{}{}
		clientCount := len(s.clients)
		s.mu.Unlock()

		s.logger.Info("price feed: MT5 client connected",
			"remote", conn.RemoteAddr(),
			"total_clients", clientCount,
		)
	}
}
