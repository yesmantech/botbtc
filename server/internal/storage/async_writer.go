package storage

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Event represents a loggable event.
type Event struct {
	Type      string      `json:"type"`      // "signal", "order", "fill", "position", "latency", "error"
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// AsyncWriter buffers events and writes them asynchronously as JSONL.
// For MVP it writes to a JSON Lines file (one JSON object per line).
// PostgreSQL integration is planned for later.
type AsyncWriter struct {
	eventCh       chan Event
	file          *os.File
	filePath      string
	flushInterval time.Duration
	batchSize     int
	logger        *slog.Logger
	wg            sync.WaitGroup
}

// NewAsyncWriter creates a new async writer that appends JSONL to filePath.
func NewAsyncWriter(filePath string, bufferSize int, flushIntervalMs int, logger *slog.Logger) (*AsyncWriter, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &AsyncWriter{
		eventCh:       make(chan Event, bufferSize),
		file:          f,
		filePath:      filePath,
		flushInterval: time.Duration(flushIntervalMs) * time.Millisecond,
		batchSize:     bufferSize,
		logger:        logger,
	}, nil
}

// Write queues an event for async writing. Non-blocking; drops if buffer full.
func (w *AsyncWriter) Write(event Event) {
	select {
	case w.eventCh <- event:
	default:
		w.logger.Warn("async_writer: event dropped, buffer full", "type", event.Type)
	}
}

// WriteSignal is a convenience method for signal events.
func (w *AsyncWriter) WriteSignal(signal interface{}) {
	w.Write(Event{
		Type:      "signal",
		Timestamp: time.Now(),
		Data:      signal,
	})
}

// WriteOrder is a convenience method for order events.
func (w *AsyncWriter) WriteOrder(order interface{}) {
	w.Write(Event{
		Type:      "order",
		Timestamp: time.Now(),
		Data:      order,
	})
}

// WriteLatency is a convenience method for latency events.
func (w *AsyncWriter) WriteLatency(record interface{}) {
	w.Write(Event{
		Type:      "latency",
		Timestamp: time.Now(),
		Data:      record,
	})
}

// WriteError is a convenience method for error events.
func (w *AsyncWriter) WriteError(errType, message string, ctx map[string]string) {
	w.Write(Event{
		Type:      "error",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"error_type": errType,
			"message":    message,
			"context":    ctx,
		},
	})
}

// Start begins the background writer goroutine. It collects events from the
// channel, buffers up to batchSize, and flushes when the buffer is full or
// flushInterval fires.
func (w *AsyncWriter) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
}

// Stop flushes remaining events and closes the file.
func (w *AsyncWriter) Stop() {
	close(w.eventCh)
	w.wg.Wait()
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func (w *AsyncWriter) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, w.batchSize)

	for {
		select {
		case ev, ok := <-w.eventCh:
			if !ok {
				// Channel closed — flush remaining and exit.
				if len(batch) > 0 {
					w.flush(batch)
				}
				w.closeFile()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= w.batchSize {
				w.flush(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}

		case <-ctx.Done():
			// Drain remaining events from channel.
			for ev := range w.eventCh {
				batch = append(batch, ev)
			}
			if len(batch) > 0 {
				w.flush(batch)
			}
			w.closeFile()
			return
		}
	}
}

func (w *AsyncWriter) flush(batch []Event) {
	for i := range batch {
		data, err := json.Marshal(batch[i])
		if err != nil {
			w.logger.Error("async_writer: marshal error", "err", err)
			continue
		}
		data = append(data, '\n')
		if _, err := w.file.Write(data); err != nil {
			w.logger.Error("async_writer: write error", "err", err)
		}
	}
	if err := w.file.Sync(); err != nil {
		w.logger.Error("async_writer: sync error", "err", err)
	}
}

func (w *AsyncWriter) closeFile() {
	if err := w.file.Close(); err != nil {
		w.logger.Error("async_writer: close error", "err", err)
	}
}
