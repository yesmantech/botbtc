package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/botbtc/server/internal/model"
)

// Server is a TCP server that receives trading signals from the MT5 bridge.
type Server struct {
	host           string
	port           int
	readTimeout    time.Duration
	maxConnections int

	listener   net.Listener
	signalCh   chan *model.Signal
	activeConns atomic.Int64

	wg     sync.WaitGroup
	cancel context.CancelFunc
	logger *slog.Logger
}

// NewServer creates a new bridge TCP server.
func NewServer(host string, port int, readTimeoutMs int, maxConnections int, logger *slog.Logger) *Server {
	return &Server{
		host:           host,
		port:           port,
		readTimeout:    time.Duration(readTimeoutMs) * time.Millisecond,
		maxConnections: maxConnections,
		signalCh:       make(chan *model.Signal, 64),
		logger:         logger,
	}
}

// SignalChan returns the channel on which parsed signals are published.
func (s *Server) SignalChan() <-chan *model.Signal {
	return s.signalCh
}

// Start begins listening for TCP connections. It blocks until the context is
// cancelled or an unrecoverable error occurs.
func (s *Server) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bridge listen on %s: %w", addr, err)
	}
	s.listener = ln
	s.logger.Info("bridge server listening", "addr", addr, "max_connections", s.maxConnections)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ctx)
	}()

	return nil
}

// Stop gracefully shuts down the bridge server and waits for in-flight
// connections to drain.
func (s *Server) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	close(s.signalCh)
	s.logger.Info("bridge server stopped")
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.logger.Error("bridge accept error", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if int(s.activeConns.Load()) >= s.maxConnections {
			s.logger.Warn("bridge max connections reached, rejecting", "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}

		s.activeConns.Add(1)
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer s.activeConns.Add(-1)
			s.handleConnection(ctx, c)
		}(conn)
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	remote := conn.RemoteAddr().String()
	s.logger.Info("bridge connection accepted", "remote", remote)
	defer func() {
		_ = conn.Close()
		s.logger.Info("bridge connection closed", "remote", remote)
	}()

	scanner := bufio.NewScanner(conn)
	// Allow up to 64KB per message.
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if s.readTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // read timeout, keep connection alive
				}
				s.logger.Error("bridge scan error", "remote", remote, "error", err)
			}
			return // EOF or fatal error
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		t2 := time.Now().UnixMilli()

		sig, err := ParseSignal(line)
		if err != nil {
			if errors.Is(err, ErrHeartbeat) {
				s.logger.Debug("bridge heartbeat received", "remote", remote)
				continue
			}
			s.logger.Error("bridge parse error", "remote", remote, "error", err)
			ack, _ := BuildACK("", "ERROR")
			_, _ = conn.Write(ack)
			continue
		}

		s.logger.Info("bridge signal received",
			"signal_id", sig.SignalID,
			"side", sig.Side,
			"symbol", sig.Symbol,
			"t2_recv_ms", t2,
		)

		// Send ACK immediately.
		ack, err := BuildACK(sig.SignalID, "OK")
		if err != nil {
			s.logger.Error("bridge ack build error", "signal_id", sig.SignalID, "error", err)
			continue
		}
		if _, err := conn.Write(ack); err != nil {
			s.logger.Error("bridge ack write error", "signal_id", sig.SignalID, "error", err)
			return
		}

		// Forward signal to processing pipeline.
		select {
		case s.signalCh <- sig:
		case <-ctx.Done():
			return
		}
	}
}
