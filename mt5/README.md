# MT5 Signal Generator — BTC Scalper v2.0

Tick-level signal generator for MetaTrader 5.  
Generates **LONG / SHORT** signals based on micro-structure features and streams them as newline-delimited JSON over a TCP socket to a Python bridge. **No trades are executed inside MT5.**

---

## Architecture

```
┌──────────────┐  tick  ┌───────────────┐  JSON/TCP  ┌──────────────┐
│   MT5 Server │──────▶│  EA (this)     │──────────▶│ Python Bridge│
│   (broker)   │       │  signal only   │◀─── ACK ──│              │
└──────────────┘       └───────────────┘            └──────────────┘
```

1. **FeatureEngine** — ring buffer (2 000 ticks), computes velocity (250 / 500 / 1000 ms), acceleration, micro-EMA, micro-ATR. Exposes `IsReady()`, `GetMidPrice()`, `GetLatestTickTimeMs()`.
2. **SignalEngine** — evaluates features against thresholds → `SIGNAL_LONG`, `SIGNAL_SHORT`, or `SIGNAL_NONE`. Enforces cooldown between signals and minimum tick count readiness.
3. **BridgeClient** — TCP socket client (`SocketCreate` / `SocketConnect` / `SocketSend` / `SocketRead`), with throttled auto-reconnect, message counters, and uptime tracking.
4. **JsonSerializer** — lightweight `CJsonBuilder` class, no external dependencies.

---

## What's New in v2.0

| Feature                  | Description                                                                 |
|--------------------------|-----------------------------------------------------------------------------|
| **Symbol Info Fields**   | `contract_size`, `volume_min`, `volume_max`, `volume_step`, `point`, `digits` added to every signal JSON |
| **Cooldown Mechanism**   | Configurable `CooldownMs` (default 1000 ms) prevents signal flooding        |
| **Kill Switch**          | Press **K** on the chart to toggle signal generation on/off                 |
| **MinTickCount**         | Engine won't generate signals until `MinTickCount` ticks are buffered       |
| **Connection Stats**     | Bridge tracks `messages_sent`, `messages_received`, and `uptime_ms`         |
| **Live Chart Comment**   | Chart overlay shows real-time status, tick count, signal count, bridge state |
| **Enhanced Heartbeat**   | Heartbeat includes bridge stats, kill switch state, and uptime              |
| **Better Error Logging** | All error conditions use `Print()`/`PrintFormat()` with context             |

---

## File Layout

```
mt5/
├── Experts/
│   └── BTC_Scalper_Signal.mq5     # Main EA (v2.0)
├── Include/
│   ├── BridgeClient.mqh           # TCP socket client + stats
│   ├── FeatureEngine.mqh          # Tick ring buffer & features
│   ├── JsonSerializer.mqh         # JSON builder
│   └── SignalEngine.mqh           # Signal evaluation + cooldown
└── README.md                      # ← you are here
```

---

## Installation

1. Open your **MetaTrader 5** data folder:  
   *File → Open Data Folder* (or `%APPDATA%\MetaQuotes\Terminal\<ID>\`).

2. Copy files into the data folder, mirroring the layout above:
   - `Experts/BTC_Scalper_Signal.mq5` → `MQL5/Experts/`
   - `Include/*.mqh` → `MQL5/Include/`

3. In MetaEditor, **compile** `BTC_Scalper_Signal.mq5` (F7). There should be **zero** errors.

4. In MT5, enable sockets:  
   *Tools → Options → Expert Advisors* → ✅ **Allow algorithmic trading**  
   Add `127.0.0.1` (or your bridge host) to the allowed URLs/addresses list.

5. Drag `BTC_Scalper_Signal` onto a **BTCUSD** (or equivalent) chart.

---

## Input Parameters

| Parameter            | Default       | Description                                   |
|----------------------|---------------|-----------------------------------------------|
| `BridgeHost`         | `127.0.0.1`   | TCP host of the Python bridge                 |
| `BridgePort`         | `9090`        | TCP port of the Python bridge                 |
| `SocketTimeout`      | `3000`        | Socket connect timeout (ms)                   |
| `ReconnectIntervalSec`| `5`          | Seconds between reconnect attempts            |
| `MinVelocity250`     | `5.0`         | Min absolute velocity over 250 ms             |
| `MinVelocity500`     | `3.0`         | Min absolute velocity over 500 ms             |
| `MaxSpread`          | `50.0`        | Max bid–ask spread allowed (points)           |
| `MinATR`             | `2.0`         | Min micro-ATR (1 s) to generate signal        |
| `MaxATR`             | `200.0`       | Max micro-ATR (1 s) to generate signal        |
| `MagicNumber`        | `20260521`    | Unique ID embedded in signal IDs              |
| `RingBufferSize`     | `2000`        | Tick ring buffer capacity                     |
| `EmaFastPeriod`      | `10.0`        | EMA fast smoothing period (tick-based)         |
| `EmaSlowPeriod`      | `30.0`        | EMA slow smoothing period (tick-based)         |
| `CooldownMs`         | `1000`        | Minimum milliseconds between consecutive signals |
| `MinTickCount`       | `100`         | Minimum ticks before engine starts generating signals |

---

## JSON Signal Format

Each signal is a single JSON object terminated by `\n`:

```json
{
  "signal_id":      "SIG-20260521-20260521-000001",
  "symbol":         "BTCUSD",
  "signal":         "LONG",
  "bid":            67432.50,
  "ask":            67435.00,
  "spread":         2.50,
  "velocity250":    7.2300,
  "velocity500":    4.1200,
  "velocity1000":   2.8100,
  "acceleration":   4.4200,
  "ema_fast":       67430.1234,
  "ema_slow":       67428.5678,
  "micro_atr_1s":   3.4500,
  "micro_atr_2s":   4.1200,
  "contract_size":  1.00000000,
  "volume_min":     0.01000000,
  "volume_max":     100.00000000,
  "volume_step":    0.01000000,
  "point":          0.01000000,
  "digits":         2,
  "timestamp_ms":   1716300000123,
  "magic":          20260521
}
```

---

## Kill Switch

Press **K** on the chart at any time to toggle signal generation:

- **K pressed once** → signals paused, chart shows `STOPPED`
- **K pressed again** → signals resumed, chart shows `ACTIVE`

The kill switch state is also included in heartbeat messages (`"kill_switch": true/false`).

---

## Connection Stats

The bridge client tracks:

| Metric               | Description                                      |
|----------------------|--------------------------------------------------|
| `messages_sent`      | Total JSON messages sent (signals + heartbeats)  |
| `messages_received`  | Total ACK/response messages received             |
| `uptime_ms`          | Duration of current connection in milliseconds   |

These stats are visible in the chart comment and included in heartbeat payloads.

---

## Dependencies

**None.** Pure MQL5 — no external libraries, DLLs, or `WebRequest`.

---

## Important Notes

- This EA **does not place any orders**. It is a **signal-only** generator.
- The Python bridge must be running and listening on the configured host:port **before** the EA starts (or the EA will retry automatically).
- Heartbeat messages (`{"type":"heartbeat", ...}`) are sent every second on the timer.
- All timestamps use **milliseconds** (`MqlTick.time_msc` / `GetTickCount64()`).
- The `MinTickCount` parameter prevents premature signals during the warm-up phase.
- The `CooldownMs` parameter prevents signal flooding during fast market conditions.
