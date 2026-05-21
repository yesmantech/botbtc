package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the scalper server.
type Config struct {
	Bridge     BridgeConfig     `yaml:"bridge"`
	BingX      BingXConfig      `yaml:"bingx"`
	Risk       RiskConfig       `yaml:"risk"`
	Trade      TradeConfig      `yaml:"trade"`
	Trailing   TrailingConfig   `yaml:"trailing"`
	Latency    LatencyConfig    `yaml:"latency"`
	Orders     OrdersConfig     `yaml:"orders"`
	Quantity   QuantityConfig   `yaml:"quantity"`
	Logging    LoggingConfig    `yaml:"logging"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Storage    StorageConfig    `yaml:"storage"`
}

// BridgeConfig configures the TCP bridge to MT5.
type BridgeConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	ReadTimeoutMs  int    `yaml:"read_timeout_ms"`
	MaxConnections int    `yaml:"max_connections"`
}

// BingXConfig holds exchange API credentials and endpoints.
type BingXConfig struct {
	APIKey       string `yaml:"api_key"`
	APISecret    string `yaml:"api_secret"`
	BaseURL      string `yaml:"base_url"`
	WsMarketURL  string `yaml:"ws_market_url"`
	WsAccountURL string `yaml:"ws_account_url"`
	RecvWindow   int    `yaml:"recv_window"`
	Symbol       string `yaml:"symbol"`
	AccountID    string `yaml:"account_id"`
	PaperMode    bool   `yaml:"paper_mode"`
}

// RiskConfig defines risk management parameters.
type RiskConfig struct {
	BaseRiskPercent                float64 `yaml:"base_risk_percent"`
	EscalatedRiskPercent           float64 `yaml:"escalated_risk_percent"`
	EscalateAfterConsecutiveLosses int     `yaml:"escalate_after_consecutive_losses"`
	MaxDailyTrades                 int     `yaml:"max_daily_trades"`
	MaxDailyStopLosses             int     `yaml:"max_daily_stop_losses"`
	MaxOpenPositions               int     `yaml:"max_open_positions"`
}

// TradeConfig defines trade execution parameters.
type TradeConfig struct {
	MaxTradeDurationMs    int     `yaml:"max_trade_duration_ms"`
	InitialStopLossUSD    float64 `yaml:"initial_stop_loss_usd"`
	MaxTakeProfitUSD      float64 `yaml:"max_take_profit_usd"`
	TimeStopNoProgressMs  int     `yaml:"time_stop_no_progress_ms"`
	MinFavorableMoveUSD   float64 `yaml:"min_favorable_move_usd"`
}

// TrailingStep defines a single trailing stop step.
type TrailingStep struct {
	ProfitUSD float64 `yaml:"profit_usd"`
	LockUSD   float64 `yaml:"lock_usd"`
}

// TrailingConfig configures the trailing stop mechanism.
type TrailingConfig struct {
	Enabled       bool           `yaml:"enabled"`
	ActivationUSD float64        `yaml:"activation_usd"`
	Steps         []TrailingStep `yaml:"steps"`
}

// LatencyConfig defines maximum acceptable latencies.
type LatencyConfig struct {
	MaxSignalAgeMs     int `yaml:"max_signal_age_ms"`
	MaxMT5ToBridgeMs   int `yaml:"max_mt5_to_bridge_ms"`
	MaxSignalToAckMs   int `yaml:"max_signal_to_ack_ms"`
	MaxSignalToFillMs  int `yaml:"max_signal_to_fill_ms"`
}

// OrdersConfig configures order execution behaviour.
type OrdersConfig struct {
	OrderType              string  `yaml:"order_type"`
	MakerProbeTimeoutMs    int     `yaml:"maker_probe_timeout_ms"`
	AggressiveIOCTimeoutMs int     `yaml:"aggressive_ioc_timeout_ms"`
	MaxTotalEntryWindowMs  int     `yaml:"max_total_entry_window_ms"`
	MaxRepriceAttempts     int     `yaml:"max_reprice_attempts"`
	MaxEntrySlippageUSD    float64 `yaml:"max_entry_slippage_usd"`
	MaxExitSlippageUSD     float64 `yaml:"max_exit_slippage_usd"`
}

// LoggingConfig configures structured logging.
type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

// MonitoringConfig configures Prometheus and alerting.
type MonitoringConfig struct {
	PrometheusPort   int    `yaml:"prometheus_port"`
	EnableAlerts     bool   `yaml:"enable_alerts"`
	TelegramBotToken string `yaml:"telegram_bot_token"`
	TelegramChatID   string `yaml:"telegram_chat_id"`
}

// StorageConfig configures the async event writer.
type StorageConfig struct {
	EventLogFile    string `yaml:"event_log_file"`
	BufferSize      int    `yaml:"buffer_size"`
	FlushIntervalMs int    `yaml:"flush_interval_ms"`
}

// QuantityConfig for BingX contract rules (loaded from API at runtime, defaults here).
type QuantityConfig struct {
	MinQty          float64 `yaml:"min_qty"`          // min order quantity in BTC
	MaxQty          float64 `yaml:"max_qty"`          // max order quantity in BTC
	QtyStep         float64 `yaml:"qty_step"`         // quantity increment
	PricePrecision  int     `yaml:"price_precision"`  // decimal places for price
	QtyPrecision    int     `yaml:"qty_precision"`    // decimal places for quantity
	TickSize        float64 `yaml:"tick_size"`        // minimum price movement
	MaxLeverage     int     `yaml:"max_leverage"`     // maximum allowed leverage
	DefaultLeverage int     `yaml:"default_leverage"` // default leverage to use
}

// LoadConfig reads and parses a YAML config file at the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	return &cfg, nil
}
