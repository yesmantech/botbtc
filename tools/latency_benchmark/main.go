package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	bingxBaseURL   = "https://open-api.bingx.com"
	serverTimePath = "/openApi/swap/v2/server/time"
	bookTickerPath = "/openApi/swap/v2/quote/bookTicker"

	totalRequests = 100
	warmupRounds  = 5
	pauseBetween  = 50 * time.Millisecond // avoid rate limit
)

type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║     BingX API LATENCY BENCHMARK                     ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()

	client := &http.Client{Timeout: 10 * time.Second}

	// ── Warmup ───────────────────────────────────────────────────
	fmt.Printf("Warming up (%d requests)...\n", warmupRounds)
	for i := 0; i < warmupRounds; i++ {
		_ = pingServerTime(client)
		time.Sleep(pauseBetween)
	}

	// ── Test 1: Server Time endpoint ─────────────────────────────
	fmt.Printf("\n── Test 1: GET %s (%d requests) ──\n", serverTimePath, totalRequests)
	serverTimeLatencies := benchmark(client, bingxBaseURL+serverTimePath, totalRequests)
	printStats("Server Time", serverTimeLatencies)

	// ── Test 2: Book Ticker endpoint ─────────────────────────────
	bookURL := bingxBaseURL + bookTickerPath + "?symbol=BTC-USDT"
	fmt.Printf("\n── Test 2: GET %s (%d requests) ──\n", bookTickerPath, totalRequests)
	bookTickerLatencies := benchmark(client, bookURL, totalRequests)
	printStats("Book Ticker", bookTickerLatencies)

	// ── Test 3: Clock delta measurement ──────────────────────────
	fmt.Println("\n── Test 3: Clock Delta (local vs BingX server) ──")
	measureClockDelta(client)

	// ── Test 4: Jitter analysis ──────────────────────────────────
	fmt.Println("\n── Test 4: Jitter Analysis (Server Time) ──")
	printJitter(serverTimeLatencies)

	// ── Summary ──────────────────────────────────────────────────
	hostname, _ := os.Hostname()
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║     SUMMARY                                         ║")
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Printf("║  Host:       %-40s║\n", hostname)
	fmt.Printf("║  Target:     %-40s║\n", bingxBaseURL)
	fmt.Printf("║  Requests:   %-40d║\n", totalRequests)
	fmt.Printf("║  Server Time p50: %-35s║\n", fmtMs(percentile(serverTimeLatencies, 50)))
	fmt.Printf("║  Server Time p95: %-35s║\n", fmtMs(percentile(serverTimeLatencies, 95)))
	fmt.Printf("║  Server Time p99: %-35s║\n", fmtMs(percentile(serverTimeLatencies, 99)))
	fmt.Printf("║  Book Ticker p50: %-35s║\n", fmtMs(percentile(bookTickerLatencies, 50)))
	fmt.Printf("║  Book Ticker p95: %-35s║\n", fmtMs(percentile(bookTickerLatencies, 95)))
	fmt.Printf("║  Book Ticker p99: %-35s║\n", fmtMs(percentile(bookTickerLatencies, 99)))
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	// ── Assessment ───────────────────────────────────────────────
	p50 := percentile(serverTimeLatencies, 50)
	p99 := percentile(serverTimeLatencies, 99)
	fmt.Println()
	if p50 < 15 && p99 < 50 {
		fmt.Println("✅ EXCELLENT — This region is suitable for scalping.")
	} else if p50 < 30 && p99 < 80 {
		fmt.Println("⚠️  ACCEPTABLE — Usable but not ideal. Consider a closer region.")
	} else {
		fmt.Println("❌ TOO SLOW — This region is NOT suitable for low-latency scalping.")
	}
	fmt.Println()
}

// benchmark sends N GET requests and returns latencies in ms.
func benchmark(client *http.Client, url string, n int) []float64 {
	latencies := make([]float64, 0, n)
	errors := 0

	for i := 0; i < n; i++ {
		start := time.Now()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		resp, err := client.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			errors++
			fmt.Printf("  [%3d] ERROR: %v\n", i+1, err)
			time.Sleep(pauseBetween)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		ms := float64(elapsed.Microseconds()) / 1000.0
		latencies = append(latencies, ms)

		if i%20 == 0 {
			fmt.Printf("  [%3d/%d] %.2fms\n", i+1, n, ms)
		}

		time.Sleep(pauseBetween)
	}

	if errors > 0 {
		fmt.Printf("  ⚠ %d errors out of %d requests\n", errors, n)
	}

	return latencies
}

func printStats(name string, latencies []float64) {
	if len(latencies) == 0 {
		fmt.Println("  No data.")
		return
	}

	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	sort.Float64s(sorted)

	min := sorted[0]
	max := sorted[len(sorted)-1]
	avg := mean(sorted)

	fmt.Printf("\n  %-15s Results:\n", name)
	fmt.Printf("  ├─ Min:     %8.2fms\n", min)
	fmt.Printf("  ├─ Max:     %8.2fms\n", max)
	fmt.Printf("  ├─ Avg:     %8.2fms\n", avg)
	fmt.Printf("  ├─ p50:     %8.2fms\n", percentile(sorted, 50))
	fmt.Printf("  ├─ p75:     %8.2fms\n", percentile(sorted, 75))
	fmt.Printf("  ├─ p90:     %8.2fms\n", percentile(sorted, 90))
	fmt.Printf("  ├─ p95:     %8.2fms\n", percentile(sorted, 95))
	fmt.Printf("  ├─ p99:     %8.2fms\n", percentile(sorted, 99))
	fmt.Printf("  └─ StdDev:  %8.2fms\n", stddev(sorted))
}

func printJitter(latencies []float64) {
	if len(latencies) < 2 {
		fmt.Println("  Not enough data.")
		return
	}

	jitters := make([]float64, len(latencies)-1)
	for i := 1; i < len(latencies); i++ {
		jitters[i-1] = math.Abs(latencies[i] - latencies[i-1])
	}

	sort.Float64s(jitters)
	fmt.Printf("  ├─ Jitter Avg:  %8.2fms\n", mean(jitters))
	fmt.Printf("  ├─ Jitter p50:  %8.2fms\n", percentile(jitters, 50))
	fmt.Printf("  ├─ Jitter p95:  %8.2fms\n", percentile(jitters, 95))
	fmt.Printf("  └─ Jitter Max:  %8.2fms\n", jitters[len(jitters)-1])
}

func measureClockDelta(client *http.Client) {
	deltas := make([]int64, 0, 10)

	for i := 0; i < 10; i++ {
		beforeMs := time.Now().UnixMilli()
		serverMs := pingServerTime(client)
		afterMs := time.Now().UnixMilli()

		if serverMs > 0 {
			roundTrip := afterMs - beforeMs
			estimated := serverMs + roundTrip/2
			delta := afterMs - estimated
			deltas = append(deltas, delta)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if len(deltas) == 0 {
		fmt.Println("  Failed to measure clock delta.")
		return
	}

	var sum int64
	for _, d := range deltas {
		sum += d
	}
	avgDelta := sum / int64(len(deltas))

	fmt.Printf("  ├─ Samples:     %d\n", len(deltas))
	fmt.Printf("  ├─ Avg Delta:   %dms (local - server)\n", avgDelta)
	if abs64(avgDelta) < 50 {
		fmt.Println("  └─ ✅ Clock sync OK")
	} else {
		fmt.Println("  └─ ⚠️  Clock drift detected — consider NTP sync")
	}
}

func pingServerTime(client *http.Client) int64 {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, bingxBaseURL+serverTimePath, nil)
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return 0
	}

	var data struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return 0
	}
	return data.ServerTime
}

// ── Math helpers ─────────────────────────────────────────────────

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	s := make([]float64, len(sorted))
	copy(s, sorted)
	sort.Float64s(s)

	idx := (p / 100.0) * float64(len(s)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(s) {
		return s[lower]
	}
	frac := idx - float64(lower)
	return s[lower]*(1-frac) + s[upper]*frac
}

func mean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

func stddev(data []float64) float64 {
	if len(data) < 2 {
		return 0
	}
	m := mean(data)
	sum := 0.0
	for _, v := range data {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(data)-1))
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func fmtMs(ms float64) string {
	return fmt.Sprintf("%.2fms", ms)
}
