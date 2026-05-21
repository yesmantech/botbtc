package orders

import (
	"fmt"
	"sync"
	"time"
)

var (
	idMu sync.Mutex
)

// GenerateClientOrderID creates a deterministic, human-readable order ID.
// Format: btcscalp-{accountID}-{YYYYMMDD}-{signalID[-6:]}-{role}-{attempt:02d}
func GenerateClientOrderID(accountID, signalID, role string, attempt int) string {
	idMu.Lock()
	defer idMu.Unlock()

	date := time.Now().UTC().Format("20060102")

	suffix := signalID
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}

	return fmt.Sprintf("btcscalp-%s-%s-%s-%s-%02d",
		accountID, date, suffix, role, attempt,
	)
}
