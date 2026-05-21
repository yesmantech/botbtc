package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// AlertSender delivers notifications via Telegram Bot API.
// When disabled or when botToken is empty it falls back to logging.
type AlertSender struct {
	botToken   string
	chatID     string
	enabled    bool
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAlertSender creates a new Telegram alert sender.
func NewAlertSender(botToken, chatID string, enabled bool, logger *slog.Logger) *AlertSender {
	return &AlertSender{
		botToken: botToken,
		chatID:   chatID,
		enabled:  enabled,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// SendAlert sends a generic alert message.
func (a *AlertSender) SendAlert(ctx context.Context, level, message string) error {
	text := fmt.Sprintf("<b>[%s]</b>\n%s", level, message)
	return a.send(ctx, text)
}

// SendKillSwitch sends an urgent kill-switch notification.
func (a *AlertSender) SendKillSwitch(ctx context.Context, reason string) error {
	text := fmt.Sprintf("🚨 <b>KILL SWITCH ACTIVATED</b>\n\nReason: %s\n\nAll trading halted. Manual intervention required.", reason)
	return a.send(ctx, text)
}

// SendTradeResult sends a trade completion summary.
func (a *AlertSender) SendTradeResult(ctx context.Context, signalID, side string, pnlUSD float64, durationMs int64) error {
	emoji := "🟢"
	if pnlUSD < 0 {
		emoji = "🔴"
	}
	text := fmt.Sprintf("%s <b>Trade Closed</b>\n\nSignal: <code>%s</code>\nSide: %s\nPnL: $%.2f\nDuration: %dms",
		emoji, signalID, side, pnlUSD, durationMs)
	return a.send(ctx, text)
}

// SendDailySummary sends an end-of-day summary.
func (a *AlertSender) SendDailySummary(ctx context.Context, trades, wins, losses int, totalPnL float64) error {
	emoji := "📊"
	if totalPnL >= 0 {
		emoji = "📈"
	} else {
		emoji = "📉"
	}
	winRate := 0.0
	if trades > 0 {
		winRate = float64(wins) / float64(trades) * 100
	}
	text := fmt.Sprintf("%s <b>Daily Summary</b>\n\nTrades: %d\nWins: %d | Losses: %d\nWin Rate: %.1f%%\nTotal PnL: $%.2f",
		emoji, trades, wins, losses, winRate, totalPnL)
	return a.send(ctx, text)
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

// telegramPayload matches the Telegram Bot API sendMessage request.
type telegramPayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

func (a *AlertSender) send(ctx context.Context, text string) error {
	if !a.enabled || a.botToken == "" {
		a.logger.Info("alert (not sent)", "text", text)
		return nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", a.botToken)

	payload := telegramPayload{
		ChatID:    a.chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	a.logger.Debug("alert sent", "status", resp.StatusCode)
	return nil
}
