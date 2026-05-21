#!/bin/bash
# ─────────────────────────────────────────────────────────────────
# signal-tester.sh — Simulates MT5 signals to test the Go server
# ─────────────────────────────────────────────────────────────────
#
# Sends fake BUY/SELL signals via TCP to the bridge server,
# exactly like the MT5 EA would. Used for testing without MT5.
#
# Usage:
#   ./signal-tester.sh              # sends 1 BUY signal
#   ./signal-tester.sh sell         # sends 1 SELL signal
#   ./signal-tester.sh buy 5        # sends 5 BUY signals (1 per second)
# ─────────────────────────────────────────────────────────────────

HOST="${BRIDGE_HOST:-127.0.0.1}"
PORT="${BRIDGE_PORT:-9090}"
SIDE="${1:-BUY}"
COUNT="${2:-1}"
SIDE=$(echo "$SIDE" | tr '[:lower:]' '[:upper:]')

echo "╔══════════════════════════════════════════════╗"
echo "║  MT5 Signal Tester                          ║"
echo "╠══════════════════════════════════════════════╣"
echo "║  Target: ${HOST}:${PORT}"
echo "║  Side:   ${SIDE}"
echo "║  Count:  ${COUNT}"
echo "╚══════════════════════════════════════════════╝"
echo ""

for i in $(seq 1 $COUNT); do
    NOW_MS=$(date +%s%3N 2>/dev/null || python3 -c "import time; print(int(time.time()*1000))")
    SIGNAL_ID="test-$(date +%Y%m%d%H%M%S)-${i}"
    
    # Simulate a realistic BTC price
    BTC_PRICE="107500.50"
    
    # Build the JSON signal (same format as MT5 EA)
    SIGNAL=$(cat << EOF
{"signal_id":"${SIGNAL_ID}","side":"${SIDE}","symbol":"BTC-USDT","signal_price":${BTC_PRICE},"stop_loss_usd":10.0,"take_profit_usd":50.0,"confidence":0.85,"t0_tick_ms":${NOW_MS},"t1_signal_ms":${NOW_MS},"features":{"velocity":0.15,"spread":1.20,"imbalance":0.65,"vol_ratio":1.10}}
EOF
)
    # Remove newlines
    SIGNAL=$(echo "$SIGNAL" | tr -d '\n')
    
    echo "[${i}/${COUNT}] Sending ${SIDE} signal: ${SIGNAL_ID}"
    
    # Send via TCP and read response (with 2 second timeout)
    RESPONSE=$(echo "$SIGNAL" | timeout 2 nc -q 1 "$HOST" "$PORT" 2>/dev/null || echo "CONNECTION_FAILED")
    
    if [ "$RESPONSE" = "CONNECTION_FAILED" ]; then
        echo "  ❌ Connection failed — is the server running on ${HOST}:${PORT}?"
    else
        echo "  ✅ Response: ${RESPONSE}"
    fi
    
    if [ "$i" -lt "$COUNT" ]; then
        sleep 1
    fi
done

echo ""
echo "Done! Check server logs for processing details."
