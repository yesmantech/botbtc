#!/bin/bash
# ─────────────────────────────────────────────────────────────────────
# start_all.sh — Starts the complete BTC scalper stack
# ─────────────────────────────────────────────────────────────────────
# Usage: ./start_all.sh
#
# This script starts:
#   1. Go scalper server (bridge :9090, price feed :9091, metrics :9100)
#   2. MT5 via Wine (headless with Xvfb virtual display)
#
# Logs:
#   - Go server: /tmp/scalper.log
#   - MT5: /tmp/mt5.log
# ─────────────────────────────────────────────────────────────────────
set -e

export PATH="/usr/local/go/bin:$PATH"
export WINEPREFIX="/root/.wine"

echo "╔══════════════════════════════════════════════╗"
echo "║       BTC SCALPER — FULL STACK START         ║"
echo "╚══════════════════════════════════════════════╝"

# ── Kill existing processes ──
echo "🔄 Stopping existing processes..."
pkill -f scalper 2>/dev/null || true
pkill -f terminal64 2>/dev/null || true
pkill -f Xvfb 2>/dev/null || true
sleep 2

# ── Start virtual display ──
echo "🖥️  Starting virtual display..."
Xvfb :99 -screen 0 1280x800x24 &
export DISPLAY=:99
sleep 1

# ── Start Go server ──
echo "🚀 Starting Go scalper server..."
cd /root/botbtc/server
mkdir -p logs
nohup ./scalper --config ../configs/dev.yaml > /tmp/scalper.log 2>&1 &
GO_PID=$!
echo "   PID: $GO_PID"
sleep 3

# Verify Go server is running
if ! kill -0 $GO_PID 2>/dev/null; then
    echo "❌ Go server failed to start!"
    tail -10 /tmp/scalper.log
    exit 1
fi
echo "   ✅ Go server running"
echo "   📡 Bridge:     127.0.0.1:9090"
echo "   📊 Price Feed: 127.0.0.1:9091"
echo "   📈 Metrics:    :9100/metrics"

# ── Verify WebSocket connected ──
if grep -q "websocket connected" /tmp/scalper.log; then
    echo "   ✅ WebSocket connected to BingX"
else
    echo "   ⚠️  WebSocket not connected yet (may need a moment)"
fi

# ── Start MT5 ──
echo ""
echo "🔧 Starting MetaTrader 5 via Wine..."
MT5_DIR="$WINEPREFIX/drive_c/Program Files/MetaTrader 5"

# Start MT5 in portable mode (no login required for custom symbol)
cd "$MT5_DIR"
nohup wine terminal64.exe /portable > /tmp/mt5.log 2>&1 &
MT5_PID=$!
echo "   PID: $MT5_PID"
sleep 5

if kill -0 $MT5_PID 2>/dev/null; then
    echo "   ✅ MT5 running"
else
    echo "   ⚠️  MT5 may have exited (check /tmp/mt5.log)"
fi

# ── Status ──
echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║              STATUS SUMMARY                  ║"
echo "╠══════════════════════════════════════════════╣"

# Check ports
echo -n "║  Bridge  :9090  "
ss -tlnp | grep -q ":9090" && echo "✅ LISTENING" || echo "❌ DOWN"
echo -n "║  Feed    :9091  "
ss -tlnp | grep -q ":9091" && echo "✅ LISTENING" || echo "❌ DOWN"
echo -n "║  Metrics :9100  "
ss -tlnp | grep -q ":9100" && echo "✅ LISTENING" || echo "❌ DOWN"

echo -n "║  MT5            "
pgrep -f terminal64 > /dev/null && echo "✅ RUNNING" || echo "⚠️  NOT RUNNING"

echo "╚══════════════════════════════════════════════╝"
echo ""
echo "📋 Logs:"
echo "   Go:  tail -f /tmp/scalper.log"
echo "   MT5: tail -f /tmp/mt5.log"
