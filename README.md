# 🚀 BTC MT5 → BingX Scalper

A low-latency BTC scalping system that uses **MetaTrader 5** as a signal generator and **BingX** as the execution exchange.

> ⚠️ **This is a trading bot. Use at your own risk. Paper trade first.**

## Architecture

```
MT5 Expert Advisor          Go Execution Server           BingX Exchange
┌──────────────┐    TCP     ┌──────────────────┐   REST   ┌──────────┐
│ Tick Data    │───────────▶│ Bridge Server    │────────▶│ Perpetual│
│ Features     │  localhost │ Risk Engine      │  HTTPS  │ Swap API │
│ Signal Gen   │   :9090    │ Strategy Gate    │         │ BTC-USDT │
│ JSON + Send  │◀───────────│ Execution Pipe   │◀────────│          │
│              │    ACK     │ Position Monitor │  Orders │          │
└──────────────┘            └──────────────────┘         └──────────┘
```

## Key Features

- **Signal-only MT5**: MT5 generates signals, NEVER executes trades
- **Go execution server**: Low-latency order routing via BingX REST API
- **Limit orders only**: No market orders — PostOnly probe → IOC fallback
- **Paper mode**: Full simulation without touching the exchange
- **Risk management**: Daily limits, escalation, kill switch
- **Trailing stop**: Configurable profit-locking steps
- **Prometheus metrics**: Built-in monitoring (no external libs)
- **Telegram alerts**: Trade results, kill switch, daily summary
- **Async logging**: JSONL event log, never blocks the critical path
- **Zero external deps**: Only stdlib + yaml.v3

## Project Structure

```
botbtc/
├── server/                    # Go execution server
│   ├── cmd/scalper/main.go    # Entry point — wires everything
│   └── internal/
│       ├── bingx/             # BingX exchange client (live + paper)
│       │   ├── auth.go        # HMAC-SHA256 request signing
│       │   ├── client.go      # Abstract interface
│       │   ├── live.go        # Real REST client
│       │   ├── paper.go       # Paper trading simulator
│       │   └── ws.go          # Market data poller
│       ├── bridge/            # TCP bridge to MT5
│       ├── config/            # YAML configuration
│       ├── execution/         # Trade execution pipeline
│       │   ├── pipeline.go    # Signal → Entry → Monitor
│       │   ├── entry.go       # PostOnly → IOC entry flow
│       │   └── exit.go        # SL/TP/trailing/time stop
│       ├── model/             # Data structures (signal, order, position)
│       ├── monitoring/        # Prometheus metrics + Telegram alerts
│       ├── orders/            # Order tracking + ID generation
│       ├── quantity/          # Position sizing calculator
│       ├── risk/              # Risk engine (daily limits, escalation)
│       ├── storage/           # Async JSONL event writer
│       ├── strategygate/      # Signal validation (age, spread, dislocation)
│       └── timeutil/          # Time helpers
├── mt5/                       # MetaTrader 5 Expert Advisor
│   ├── Experts/
│   │   └── BTC_Scalper_Signal.mq5   # Main EA
│   └── Include/
│       ├── FeatureEngine.mqh  # Tick ring buffer + feature calculation
│       ├── SignalEngine.mqh   # Signal generation + cooldown
│       ├── BridgeClient.mqh   # TCP socket to Go server
│       └── JsonSerializer.mqh # JSON string builder
├── configs/                   # Environment configs
│   ├── dev.yaml               # Development (paper, debug logs)
│   ├── paper.yaml             # Paper trading (paper, info logs)
│   └── prod.yaml              # Production (live, warn logs)
├── tools/
│   └── latency_benchmark/     # BingX API latency tester
└── docker/                    # Docker + Prometheus setup
```

## Quick Start

### 1. Install Go

```bash
brew install go
```

### 2. Build

```bash
cd server
go mod tidy
go build ./...
```

### 3. Run Tests

```bash
go test ./... -v
```

### 4. Run in Paper Mode

```bash
mkdir -p logs
go run ./cmd/scalper/ --config ../configs/dev.yaml
```

### 5. Setup MT5

1. Install MetaTrader 5
2. Copy `mt5/Experts/BTC_Scalper_Signal.mq5` to your MT5 Experts folder
3. Copy `mt5/Include/*.mqh` to your MT5 Include folder
4. Compile the EA in MetaEditor
5. Attach to a BTCUSD chart
6. The EA connects to `127.0.0.1:9090` automatically

## Configuration

Edit `configs/dev.yaml` to customize:

| Section | What it controls |
|---------|-----------------|
| `bridge` | TCP host/port for MT5 connection |
| `bingx` | API keys, paper mode toggle, symbol |
| `risk` | Daily trade limits, risk %, escalation |
| `trade` | Stop loss, take profit, max duration |
| `trailing` | Profit-locking steps |
| `orders` | Timeouts, slippage limits |
| `quantity` | Position sizing rules |
| `monitoring` | Prometheus port, Telegram bot |
| `storage` | Event log file path |

## Latency Benchmark

Test your network latency to BingX:

```bash
cd tools/latency_benchmark
go build -o bingx-benchmark .
./bingx-benchmark
```

Run this from different VPS regions to find the best one.

## Safety Features

- **Kill switch**: Stops all trading immediately (press K in MT5, or programmatic)
- **Daily limits**: Max trades per day, max stop-losses per day
- **No market orders**: Only limit orders to control slippage
- **Duplicate prevention**: Order ID tracking prevents double-execution
- **Position guard**: Only 1 position open at a time
- **Async logging**: Database writes never block order execution

## License

MIT

## Disclaimer

This software is for educational purposes. Trading cryptocurrencies involves substantial risk of loss. Past performance does not guarantee future results. Use at your own risk.
