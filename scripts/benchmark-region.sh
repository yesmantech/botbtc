#!/bin/bash
# ─────────────────────────────────────────────────────────────────
# BingX Latency Benchmark — Run this on each VPS to compare regions
# ─────────────────────────────────────────────────────────────────
#
# USAGE:
#   curl -sL https://raw.githubusercontent.com/yesmantech/botbtc/main/scripts/benchmark-region.sh | bash
#
#   Or copy this script to the VPS and run:
#   chmod +x benchmark-region.sh && ./benchmark-region.sh
#
# WHAT IT DOES:
#   1. Installs Go (if not installed)
#   2. Downloads the benchmark tool
#   3. Runs 100 requests to BingX API
#   4. Saves results to a file
#   5. Prints a summary
#
# REQUIREMENTS: Linux VPS (Ubuntu/Debian/CentOS), curl, internet
# ─────────────────────────────────────────────────────────────────

set -e

REGION="${1:-unknown}"
RESULTS_FILE="benchmark_$(hostname)_$(date +%Y%m%d_%H%M%S).txt"

echo "╔══════════════════════════════════════════════════════╗"
echo "║     BingX REGION BENCHMARK                          ║"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  Host:     $(hostname)"
echo "║  Region:   ${REGION}"
echo "║  Date:     $(date -u)"
echo "║  Results:  ${RESULTS_FILE}"
echo "╚══════════════════════════════════════════════════════╝"
echo ""

# ── Step 1: Check/Install Go ─────────────────────────────────────
if command -v go &> /dev/null; then
    echo "✅ Go already installed: $(go version)"
else
    echo "📦 Installing Go..."
    GO_VERSION="1.23.4"
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then
        GO_ARCH="amd64"
    elif [ "$ARCH" = "aarch64" ]; then
        GO_ARCH="arm64"
    else
        echo "❌ Unsupported architecture: $ARCH"
        exit 1
    fi

    curl -sLO "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    rm "go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    export PATH="/usr/local/go/bin:$PATH"
    echo "✅ Go installed: $(go version)"
fi

export PATH="/usr/local/go/bin:$PATH"

# ── Step 2: Create benchmark tool ────────────────────────────────
echo ""
echo "🔨 Building benchmark tool..."

TMPDIR=$(mktemp -d)
cd "$TMPDIR"

cat > main.go << 'GOEOF'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	bingxBaseURL   = "https://open-api.bingx.com"
	serverTimePath = "/openApi/swap/v2/server/time"
	bookTickerPath = "/openApi/swap/v2/quote/bookTicker"
	totalRequests  = 100
	warmupRounds   = 5
	pauseBetween   = 50 * time.Millisecond
)

type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func main() {
	region := "unknown"
	if len(os.Args) > 1 {
		region = os.Args[1]
	}

	hostname, _ := os.Hostname()

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║     BingX API LATENCY BENCHMARK                     ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("  Host:   %s\n", hostname)
	fmt.Printf("  Region: %s\n", region)
	fmt.Println()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// ── TCP Ping ─────────────────────────────────────────────
	fmt.Println("── Test 0: TCP Connect Latency (raw network) ──")
	tcpLatencies := tcpPing("open-api.bingx.com:443", 20)
	printStats("TCP Connect", tcpLatencies)

	// ── Warmup ───────────────────────────────────────────────
	fmt.Printf("\nWarming up (%d requests)...\n", warmupRounds)
	for i := 0; i < warmupRounds; i++ {
		pingServerTime(client)
		time.Sleep(pauseBetween)
	}

	// ── Server Time ──────────────────────────────────────────
	fmt.Printf("\n── Test 1: GET %s (%d requests) ──\n", serverTimePath, totalRequests)
	stLatencies := benchmark(client, bingxBaseURL+serverTimePath, totalRequests)
	printStats("Server Time", stLatencies)

	// ── Book Ticker ──────────────────────────────────────────
	bookURL := bingxBaseURL + bookTickerPath + "?symbol=BTC-USDT"
	fmt.Printf("\n── Test 2: GET %s (%d requests) ──\n", bookTickerPath, totalRequests)
	btLatencies := benchmark(client, bookURL, totalRequests)
	printStats("Book Ticker", btLatencies)

	// ── Clock Delta ──────────────────────────────────────────
	fmt.Println("\n── Test 3: Clock Delta ──")
	measureClockDelta(client)

	// ── Jitter ───────────────────────────────────────────────
	fmt.Println("\n── Test 4: Jitter ──")
	printJitter(stLatencies)

	// ── Summary ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║     RESULTS                                         ║")
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Printf("║  Region:          %-34s║\n", region)
	fmt.Printf("║  Host:            %-34s║\n", hostname)
	fmt.Printf("║  TCP p50:         %-34s║\n", fmtMs(pct(tcpLatencies, 50)))
	fmt.Printf("║  TCP p99:         %-34s║\n", fmtMs(pct(tcpLatencies, 99)))
	fmt.Printf("║  ServerTime p50:  %-34s║\n", fmtMs(pct(stLatencies, 50)))
	fmt.Printf("║  ServerTime p95:  %-34s║\n", fmtMs(pct(stLatencies, 95)))
	fmt.Printf("║  ServerTime p99:  %-34s║\n", fmtMs(pct(stLatencies, 99)))
	fmt.Printf("║  BookTicker p50:  %-34s║\n", fmtMs(pct(btLatencies, 50)))
	fmt.Printf("║  BookTicker p95:  %-34s║\n", fmtMs(pct(btLatencies, 95)))
	fmt.Printf("║  BookTicker p99:  %-34s║\n", fmtMs(pct(btLatencies, 99)))
	fmt.Printf("║  Jitter avg:      %-34s║\n", fmtMs(jitterAvg(stLatencies)))
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	// Assessment
	p50 := pct(stLatencies, 50)
	p99 := pct(stLatencies, 99)
	fmt.Println()
	if p50 < 10 && p99 < 30 {
		fmt.Println("🏆 EXCELLENT — Ideal for scalping. Use this region!")
	} else if p50 < 20 && p99 < 50 {
		fmt.Println("✅ GREAT — Very good for scalping.")
	} else if p50 < 40 && p99 < 100 {
		fmt.Println("⚠️  OK — Acceptable but not ideal.")
	} else {
		fmt.Println("❌ TOO SLOW — Not suitable for scalping.")
	}

	// CSV line for easy comparison
	fmt.Printf("\nCSV: %s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f\n",
		region, hostname,
		pct(tcpLatencies, 50), pct(stLatencies, 50), pct(stLatencies, 95), pct(stLatencies, 99),
		pct(btLatencies, 50), jitterAvg(stLatencies))
}

func tcpPing(addr string, n int) []float64 {
	var results []float64
	for i := 0; i < n; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("  TCP ping error: %v\n", err)
			continue
		}
		conn.Close()
		results = append(results, float64(elapsed.Microseconds())/1000.0)
		time.Sleep(100 * time.Millisecond)
	}
	return results
}

func benchmark(client *http.Client, url string, n int) []float64 {
	var latencies []float64
	for i := 0; i < n; i++ {
		start := time.Now()
		req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		latencies = append(latencies, float64(elapsed.Microseconds())/1000.0)
		if i%25 == 0 {
			fmt.Printf("  [%3d/%d] %.2fms\n", i+1, n, latencies[len(latencies)-1])
		}
		time.Sleep(pauseBetween)
	}
	return latencies
}

func printStats(name string, data []float64) {
	if len(data) == 0 { return }
	s := sorted(data)
	fmt.Printf("\n  %-15s Results:\n", name)
	fmt.Printf("  ├─ Min:     %8.2fms\n", s[0])
	fmt.Printf("  ├─ Max:     %8.2fms\n", s[len(s)-1])
	fmt.Printf("  ├─ Avg:     %8.2fms\n", avg(s))
	fmt.Printf("  ├─ p50:     %8.2fms\n", pct(s, 50))
	fmt.Printf("  ├─ p95:     %8.2fms\n", pct(s, 95))
	fmt.Printf("  ├─ p99:     %8.2fms\n", pct(s, 99))
	fmt.Printf("  └─ StdDev:  %8.2fms\n", stddev(s))
}

func printJitter(data []float64) {
	if len(data) < 2 { return }
	var jitters []float64
	for i := 1; i < len(data); i++ {
		jitters = append(jitters, math.Abs(data[i]-data[i-1]))
	}
	s := sorted(jitters)
	fmt.Printf("  ├─ Avg:  %8.2fms\n", avg(s))
	fmt.Printf("  ├─ p50:  %8.2fms\n", pct(s, 50))
	fmt.Printf("  ├─ p95:  %8.2fms\n", pct(s, 95))
	fmt.Printf("  └─ Max:  %8.2fms\n", s[len(s)-1])
}

func measureClockDelta(client *http.Client) {
	var deltas []int64
	for i := 0; i < 10; i++ {
		before := time.Now().UnixMilli()
		st := pingServerTime(client)
		after := time.Now().UnixMilli()
		if st > 0 {
			rt := after - before
			deltas = append(deltas, after-(st+rt/2))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(deltas) == 0 { fmt.Println("  Failed"); return }
	var sum int64
	for _, d := range deltas { sum += d }
	d := sum / int64(len(deltas))
	fmt.Printf("  ├─ Avg Delta: %dms\n", d)
	if d < 50 && d > -50 {
		fmt.Println("  └─ ✅ Clock OK")
	} else {
		fmt.Println("  └─ ⚠️  Clock drift — run NTP sync")
	}
}

func pingServerTime(client *http.Client) int64 {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", bingxBaseURL+serverTimePath, nil)
	resp, err := client.Do(req)
	if err != nil { return 0 }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r apiResponse
	json.Unmarshal(body, &r)
	var d struct{ ServerTime int64 `json:"serverTime"` }
	json.Unmarshal(r.Data, &d)
	return d.ServerTime
}

func jitterAvg(data []float64) float64 {
	if len(data) < 2 { return 0 }
	sum := 0.0
	for i := 1; i < len(data); i++ {
		sum += math.Abs(data[i] - data[i-1])
	}
	return sum / float64(len(data)-1)
}

func sorted(d []float64) []float64 {
	s := make([]float64, len(d))
	copy(s, d)
	sort.Float64s(s)
	return s
}

func pct(data []float64, p float64) float64 {
	s := sorted(data)
	if len(s) == 0 { return 0 }
	idx := (p / 100) * float64(len(s)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi || hi >= len(s) { return s[lo] }
	f := idx - float64(lo)
	return s[lo]*(1-f) + s[hi]*f
}

func avg(d []float64) float64 {
	if len(d) == 0 { return 0 }
	s := 0.0
	for _, v := range d { s += v }
	return s / float64(len(d))
}

func stddev(d []float64) float64 {
	if len(d) < 2 { return 0 }
	m := avg(d)
	s := 0.0
	for _, v := range d { s += (v - m) * (v - m) }
	return math.Sqrt(s / float64(len(d)-1))
}

func fmtMs(ms float64) string { return fmt.Sprintf("%.2fms", ms) }
GOEOF

go mod init benchmark && go build -o bingx-bench . 2>/dev/null
echo "✅ Benchmark built"

# ── Step 3: Run ──────────────────────────────────────────────────
echo ""
echo "🚀 Running benchmark (region: ${REGION})..."
echo ""

./bingx-bench "${REGION}" 2>&1 | tee "/tmp/${RESULTS_FILE}"

echo ""
echo "📄 Results saved to: /tmp/${RESULTS_FILE}"
echo ""
echo "────────────────────────────────────────────────────"
echo "Copy the CSV line above and paste it in a spreadsheet"
echo "to compare all regions side by side."
echo "────────────────────────────────────────────────────"

# Cleanup
cd /
rm -rf "$TMPDIR"
