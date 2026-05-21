# Architecture — BTC MT5 → BingX Scalper

## Overview

The system is a **signal-execution split architecture** designed for ultra-low-latency BTC scalping:

```
MetaTrader 5 EA (Signal Generator)
    ↓ TCP socket localhost:9090
    ↓ JSON newline-delimited
Go Execution Server
    ↓ REST signed API + WebSocket
BingX Perpetual Futures
```

## Design Principles

1. **MT5 = Signal Generator ONLY** — no real trades on MT5
2. **Go = Execution Engine** — risk, orders, trailing, monitoring
3. **Database OUT of critical path** — async logging only
4. **LIMIT orders ONLY** — zero market orders, anti-slippage by design
5. **Single binary** — minimal deployment complexity
6. **In-memory state** — no Redis in v1, hot state in Go memory
7. **Idempotent orders** — deterministic `client_order_id` prevents duplicates

## Data Flow

```
T0: MT5 receives tick
T1: MT5 generates signal
T2: Bridge receives signal (TCP localhost)
T3: Go server processes signal
    → Risk engine: evaluate
    → Strategy gate: validate vs BingX book
    → Order manager: create order
T4: Order sent to BingX REST API
T5: BingX ACK received
T6: Order filled
T7: Position closed (TP/SL/trailing/timeout)
```

## Latency Budget

| Segment | Target p95 |
|---------|-----------|
| MT5 → Bridge | < 10 ms |
| Bridge → Server | < 5 ms |
| Server prep | < 10 ms |
| Signal → ACK | < 150 ms |
| Signal → Fill | < 250 ms |

## Component Responsibilities

### MT5 EA
- Tick buffer (250ms, 500ms, 1s, 2s windows)
- Feature calculation (velocity, acceleration, micro EMA, micro ATR)
- Signal generation (long/short conditions)
- TCP socket send + ACK receive

### TCP Bridge (Go)
- TCP listener on localhost
- JSON parse + validate
- Immediate ACK response
- Forward to execution pipeline via Go channel

### Risk Engine (Go)
- Daily trade counter (max 3)
- Daily stop loss counter (max 3 → halt)
- Risk escalation (1% → 2% after 2 consecutive losses)
- Position limit (max 1 open)
- Kill switch (API down, latency spike, desync)

### Order Manager (Go)
- Order state machine (NEW → SUBMITTING → ACKED → FILLED/CANCELLED)
- Idempotent `client_order_id` generation
- Entry flow: post-only probe → aggressive IOC
- Exit flow: TP limit, trailing, SL, emergency
- Cancel/replace on timeout

### Market Data Manager (Go)
- BingX WebSocket feed (bookTicker, depth)
- Best bid/ask, spread, book imbalance
- Microprice calculation
- Feed freshness monitoring

### Async Logger (Go)
- Buffered channel writer
- PostgreSQL batch inserts
- Non-blocking — never delays orders
