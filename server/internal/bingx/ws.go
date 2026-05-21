package bingx

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// MarketDataPoller polls the BingX REST API for book ticker data at a fixed
// interval. This replaces a WebSocket-based approach to avoid external
// dependencies (gorilla/websocket), using only stdlib.
type MarketDataPoller struct {
	client   Client
	symbol   string
	interval time.Duration
	bookCh   chan *BookTicker
	logger   *slog.Logger

	mu         sync.RWMutex
	latestBook *BookTicker
}

// NewMarketDataPoller creates a poller that fetches the best bid/ask every
// intervalMs milliseconds.
func NewMarketDataPoller(client Client, symbol string, intervalMs int, logger *slog.Logger) *MarketDataPoller {
	return &MarketDataPoller{
		client:   client,
		symbol:   symbol,
		interval: time.Duration(intervalMs) * time.Millisecond,
		bookCh:   make(chan *BookTicker, 64),
		logger:   logger,
	}
}

// Start begins the polling loop. It blocks until ctx is cancelled.
// Launch in a goroutine: go poller.Start(ctx)
func (p *MarketDataPoller) Start(ctx context.Context) {
	p.logger.Info("market poller started",
		"symbol", p.symbol,
		"interval_ms", p.interval.Milliseconds(),
	)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Do an initial poll immediately.
	p.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("market poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// BookChan returns a read-only channel that receives BookTicker updates
// every polling interval.
func (p *MarketDataPoller) BookChan() <-chan *BookTicker {
	return p.bookCh
}

// LatestBook returns the most recently fetched BookTicker, or nil if none yet.
// Thread-safe.
func (p *MarketDataPoller) LatestBook() *BookTicker {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.latestBook == nil {
		return nil
	}
	cp := *p.latestBook
	return &cp
}

func (p *MarketDataPoller) poll(ctx context.Context) {
	bt, err := p.client.GetBookTicker(ctx, p.symbol)
	if err != nil {
		p.logger.Warn("market poller: fetch error", "error", err)
		return
	}

	p.mu.Lock()
	p.latestBook = bt
	p.mu.Unlock()

	// Non-blocking send so a slow consumer doesn't block the poller.
	select {
	case p.bookCh <- bt:
	default:
		// Drop oldest if channel is full, then send.
		select {
		case <-p.bookCh:
		default:
		}
		select {
		case p.bookCh <- bt:
		default:
		}
	}
}
