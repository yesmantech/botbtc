#!/bin/bash
# ─────────────────────────────────────────────────────────────────────
# test_e2e.sh — End-to-end test: sends multiple signals and validates
# ─────────────────────────────────────────────────────────────────────
set -e

echo "╔══════════════════════════════════════════════╗"
echo "║    END-TO-END TEST — BTC SCALPER PIPELINE    ║"
echo "╚══════════════════════════════════════════════╝"

# Get real BTC price from BingX
REAL_PRICE=$(curl -s 'https://open-api.bingx.com/openApi/swap/v2/quote/bookTicker?symbol=BTC-USDT' | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['data']['book_ticker']['bid_price'])")
echo "🔷 Real BTC price: \$$REAL_PRICE"
echo ""

PASS=0
FAIL=0

run_test() {
    local TEST_NAME=$1
    local SIGNAL=$2
    local EXPECT=$3
    
    RESPONSE=$(printf '%s\n' "$SIGNAL" | nc -w 5 127.0.0.1 9090 2>/dev/null)
    sleep 2
    
    # Check last log line for result
    RESULT=$(tail -1 /tmp/scalper.log)
    
    if echo "$RESULT" | grep -q "$EXPECT"; then
        echo "  ✅ $TEST_NAME"
        PASS=$((PASS + 1))
    else
        echo "  ❌ $TEST_NAME"
        echo "     Expected: $EXPECT"
        echo "     Got: $(echo $RESULT | cut -c1-120)"
        FAIL=$((FAIL + 1))
    fi
    
    # Wait for position to close before next test
    sleep 3
}

NOW_MS=$(date +%s%3N)

# ── Test 1: Valid BUY signal ──
echo "─── Test 1: Valid BUY at real price ───"
SIGNAL='{"signal_id":"e2e-buy-001","side":"BUY","symbol":"BTC-USDT","signal_price":'$REAL_PRICE',"stop_loss_usd":10.0,"take_profit_usd":50.0,"confidence":0.85,"t0_tick_ms":'$NOW_MS',"t1_signal_ms":'$NOW_MS',"features":{"velocity":0.15,"spread":1.20,"imbalance":0.65,"vol_ratio":1.10}}'
run_test "BUY signal accepted and filled" "$SIGNAL" "position"

# Wait for position to fully close
sleep 5

# ── Test 2: Valid SELL signal ──
echo "─── Test 2: Valid SELL at real price ───"
NOW_MS=$(date +%s%3N)
SIGNAL='{"signal_id":"e2e-sell-001","side":"SELL","symbol":"BTC-USDT","signal_price":'$REAL_PRICE',"stop_loss_usd":10.0,"take_profit_usd":50.0,"confidence":0.90,"t0_tick_ms":'$NOW_MS',"t1_signal_ms":'$NOW_MS',"features":{"velocity":-0.15,"spread":1.20,"imbalance":0.35,"vol_ratio":1.10}}'
run_test "SELL signal accepted and filled" "$SIGNAL" "position"

sleep 5

# ── Test 3: Low confidence (should still work) ──
echo "─── Test 3: Low confidence signal ───"
NOW_MS=$(date +%s%3N)
SIGNAL='{"signal_id":"e2e-low-001","side":"BUY","symbol":"BTC-USDT","signal_price":'$REAL_PRICE',"stop_loss_usd":10.0,"take_profit_usd":50.0,"confidence":0.51,"t0_tick_ms":'$NOW_MS',"t1_signal_ms":'$NOW_MS',"features":{"velocity":0.10,"spread":1.00,"imbalance":0.55,"vol_ratio":1.05}}'
run_test "Low confidence signal handled" "$SIGNAL" "signal"

echo ""
echo "══════════════════════════════════════════════"
echo "  RESULTS: $PASS passed, $FAIL failed"
echo "══════════════════════════════════════════════"

echo ""
echo "📋 Full pipeline log:"
grep -E "pipeline|entry|exit|position|pnl" /tmp/scalper.log | tail -20
