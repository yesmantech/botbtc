#!/bin/bash
# ============================================================================
# BTC MT5 → BingX Scalper — Paper Trading Runner
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG="${PROJECT_DIR}/configs/paper.yaml"

echo "=========================================="
echo "  BTC Scalper — Paper Trading Mode"
echo "=========================================="
echo "Config: ${CONFIG}"
echo ""

# Verify environment variables
if [ -z "${BINGX_API_KEY:-}" ] || [ -z "${BINGX_API_SECRET:-}" ]; then
    echo "ERROR: BINGX_API_KEY and BINGX_API_SECRET must be set."
    echo "Export them before running:"
    echo "  export BINGX_API_KEY=your_key"
    echo "  export BINGX_API_SECRET=your_secret"
    exit 1
fi

# Create logs directory
mkdir -p "${PROJECT_DIR}/server/logs"

# Build and run
cd "${PROJECT_DIR}/server"
go build -o bin/scalper ./cmd/scalper/
echo "Build successful."
echo ""

exec ./bin/scalper -config "${CONFIG}"
