// ─────────────────────────────────────────────────────────────────────────────
// auth.go — HMAC-SHA256 Authentication for BingX API
// ─────────────────────────────────────────────────────────────────────────────
//
// WHY THIS FILE EXISTS:
// BingX (like most crypto exchanges) requires you to "sign" every private API
// request (placing orders, checking balance, etc.) so they can verify it really
// came from you and wasn't tampered with in transit.
//
// HOW IT WORKS:
// 1. You take all the parameters of your request (symbol, price, qty, etc.)
// 2. Sort them alphabetically and join them into a query string
// 3. Hash that string using HMAC-SHA256 with your API secret as the key
// 4. Attach the resulting signature to the request
//
// The exchange does the same computation on their side. If both signatures
// match, the request is authentic. If someone changed even 1 character,
// the signatures won't match and the request is rejected.
//
// ANALOGY: Think of it like a wax seal on a letter. Only you have the seal
// (your API secret), so only you can produce a valid stamp.
// ─────────────────────────────────────────────────────────────────────────────
package bingx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SignRequest generates an HMAC-SHA256 signature for BingX API authentication.
//
// How it works step by step:
//   1. Take all request parameters and build a sorted query string
//      Example: "price=50000&quantity=0.001&symbol=BTC-USDT&timestamp=1234567890"
//   2. Create an HMAC hasher using SHA-256 and your API secret as the key
//   3. Feed the query string into the hasher
//   4. Return the result as a hex string (e.g. "a1b2c3d4e5...")
//
// Parameters:
//   - apiSecret: your BingX API secret (never share this!)
//   - params:    map of all request parameters (key=value pairs)
//
// Returns: hex-encoded HMAC-SHA256 signature string
func SignRequest(apiSecret string, params map[string]string) string {
	// Step 1: Build sorted query string from all parameters.
	queryStr := BuildQueryString(params)

	// Step 2: Create HMAC-SHA256 hasher with your secret as the key.
	// HMAC = Hash-based Message Authentication Code.
	// SHA-256 = Secure Hash Algorithm producing a 256-bit (32-byte) hash.
	mac := hmac.New(sha256.New, []byte(apiSecret))

	// Step 3: Feed the query string into the hasher.
	mac.Write([]byte(queryStr))

	// Step 4: Get the hash result and convert to hex string.
	// mac.Sum(nil) returns the raw bytes, hex.EncodeToString makes it readable.
	return hex.EncodeToString(mac.Sum(nil))
}

// BuildQueryString builds a sorted, URL-encoded query string from a map of parameters.
//
// WHY SORTED? Because both you and BingX need to produce the exact same string
// to get the same signature. Maps in Go are unordered, so if you iterated
// randomly you'd get different strings each time → different signatures → rejected.
// Sorting by key ensures deterministic output every time.
//
// Example:
//   Input:  {"symbol": "BTC-USDT", "price": "50000", "quantity": "0.001"}
//   Output: "price=50000&quantity=0.001&symbol=BTC-USDT"
func BuildQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	// Extract all keys from the map.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	// Sort keys alphabetically (a, b, c...).
	sort.Strings(keys)

	// Build the query string: key1=value1&key2=value2&...
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&') // separator between key=value pairs
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	return sb.String()
}

// AddTimestamp adds a "timestamp" parameter to the params map.
//
// WHY THIS IS NEEDED:
// BingX requires every signed request to include a timestamp (in milliseconds).
// This prevents "replay attacks" — where someone intercepts your request and
// sends it again later. BingX checks that the timestamp is recent (within
// the recvWindow, typically 5 seconds) and rejects old requests.
//
// THE CLOCK PROBLEM:
// Your computer's clock might be slightly different from BingX's server clock.
// If your timestamp is off by too much, BingX rejects the request.
// serverTimeDelta = (your clock) - (BingX clock), measured at startup.
// We subtract this delta to align our timestamp with BingX's clock.
//
// Example: If your clock is 100ms ahead of BingX, serverTimeDelta = 100.
// We send: yourTime - 100 = BingX's time. Now the timestamps match.
func AddTimestamp(params map[string]string, serverTimeDelta int64) {
	localMs := time.Now().UnixMilli()          // current time in milliseconds
	adjustedMs := localMs - serverTimeDelta    // correct for clock drift
	params["timestamp"] = fmt.Sprintf("%d", adjustedMs)
}
