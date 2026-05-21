#!/bin/bash
# ============================================================================
# BTC MT5 → BingX Scalper — Development Runner
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG="${PROJECT_DIR}/configs/dev.yaml"

echo "=========================================="
echo "  BTC Scalper — Development Mode"
echo "=========================================="
echo "Config: ${CONFIG}"
echo ""

# Create logs directory
mkdir -p "${PROJECT_DIR}/server/logs"

# Build and run
cd "${PROJECT_DIR}/server"
go build -o bin/scalper ./cmd/scalper/
echo "Build successful."
echo ""

exec ./bin/scalper -config "${CONFIG}"
