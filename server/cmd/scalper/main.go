// ─────────────────────────────────────────────────────────────────────────────
// main.go — Entry Point for the BTC Scalper Execution Server
// ─────────────────────────────────────────────────────────────────────────────
//
// THIS IS WHERE EVERYTHING STARTS. This file:
//   1. Loads configuration from a YAML file (dev.yaml, paper.yaml, or prod.yaml)
//   2. Creates ALL components (exchange client, risk engine, pipeline, etc.)
//   3. Starts all background services (market poller, metrics, bridge)
//   4. Runs the main loop: receives signals from MT5 and sends them to the pipeline
//   5. Handles graceful shutdown when you press Ctrl+C
//
// HOW TO RUN:
//   go run ./cmd/scalper/ --config ../configs/dev.yaml    # paper mode (safe)
//   go run ./cmd/scalper/ --config ../configs/prod.yaml   # live mode (real money!)
//
// STARTUP ORDER (matters!):
//   1. Load config
//   2. Create BingX client (paper or live) + sync clock
//   3. Start market data poller (gets latest BTC prices)
//   4. Create risk engine, strategy gate, quantity calculator
//   5. Create execution pipeline (the brain)
//   6. Start Prometheus metrics HTTP endpoint
//   7. Start TCP bridge (listens for MT5 connections)
//   8. Start pipeline
//   9. Enter main loop → wait for signals from MT5
// ─────────────────────────────────────────────────────────────────────────────
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/botbtc/server/internal/bingx"
	"github.com/botbtc/server/internal/bridge"
	"github.com/botbtc/server/internal/config"
	"github.com/botbtc/server/internal/execution"
	"github.com/botbtc/server/internal/monitoring"
	"github.com/botbtc/server/internal/orders"
	"github.com/botbtc/server/internal/quantity"
	"github.com/botbtc/server/internal/risk"
	"github.com/botbtc/server/internal/storage"
	"github.com/botbtc/server/internal/strategygate"
)

func main() {
	configPath := flag.String("config", "configs/dev.yaml", "path to YAML config file")
	flag.Parse()

	// ── Load config ──────────────────────────────────────────────────
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	// ── Logger ───────────────────────────────────────────────────────
	logLevel := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// ── Banner ───────────────────────────────────────────────────────
	printBanner(cfg)

	// ── BingX Client (paper or live) ─────────────────────────────────
	var exchange bingx.Client
	if cfg.BingX.PaperMode {
		logger.Info("starting in PAPER mode — no real orders")
		exchange = bingx.NewPaperClient(cfg.BingX.Symbol, 10000.0, logger)
	} else {
		logger.Info("starting in LIVE mode", "base_url", cfg.BingX.BaseURL)
		liveClient := bingx.NewLiveClient(cfg.BingX, logger)
		if err := liveClient.SyncServerTime(context.Background()); err != nil {
			logger.Error("failed to sync server time", "error", err)
			os.Exit(1)
		}
		exchange = liveClient
	}

	// ── Market Data Poller ───────────────────────────────────────────
	marketPoller := bingx.NewMarketDataPoller(exchange, cfg.BingX.Symbol, 200, logger)

	// ── Core Components ──────────────────────────────────────────────
	riskEngine := risk.NewEngine(cfg.Risk, logger)
	stratGate := strategygate.NewGate(cfg.Latency, cfg.Orders, logger)
	qtyCalc := quantity.NewCalculator(cfg.Quantity, logger)
	orderMgr := orders.NewManager("scalper", logger)
	latencyTracker := monitoring.NewLatencyTracker(logger)

	// ── Execution Pipeline ───────────────────────────────────────────
	pipeline := execution.NewPipeline(
		cfg,
		riskEngine,
		stratGate,
		qtyCalc,
		orderMgr,
		latencyTracker,
		exchange,
		marketPoller,
		logger,
	)

	// ── Prometheus Metrics ───────────────────────────────────────────
	metrics := monitoring.NewMetrics()

	// ── Telegram Alerts ──────────────────────────────────────────────
	alertSender := monitoring.NewAlertSender(
		cfg.Monitoring.TelegramBotToken,
		cfg.Monitoring.TelegramChatID,
		cfg.Monitoring.EnableAlerts,
		logger,
	)
	_ = alertSender // used in pipeline exit callbacks (TODO: wire)

	// ── Async Event Writer ───────────────────────────────────────────
	eventWriter, err := storage.NewAsyncWriter(
		cfg.Storage.EventLogFile,
		cfg.Storage.BufferSize,
		cfg.Storage.FlushIntervalMs,
		logger,
	)
	if err != nil {
		logger.Error("failed to create event writer", "error", err)
		os.Exit(1)
	}

	// ── Bridge Server ────────────────────────────────────────────────
	bridgeServer := bridge.NewServer(
		cfg.Bridge.Host,
		cfg.Bridge.Port,
		cfg.Bridge.ReadTimeoutMs,
		cfg.Bridge.MaxConnections,
		logger,
	)

	// ── Startup ──────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start async event writer.
	eventWriter.Start(ctx)

	// Start market data poller.
	marketPoller.Start(ctx)

	// Start Prometheus metrics endpoint.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		addr := fmt.Sprintf(":%d", cfg.Monitoring.PrometheusPort)
		logger.Info("prometheus metrics server starting", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("metrics server error", "error", err)
		}
	}()

	// Start bridge server.
	if err := bridgeServer.Start(ctx); err != nil {
		logger.Error("failed to start bridge server", "error", err)
		os.Exit(1)
	}

	// Start execution pipeline.
	pipeline.Start(ctx)

	// ── Signal processing loop ───────────────────────────────────────
	osSigCh := make(chan os.Signal, 1)
	signal.Notify(osSigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("scalper server running, waiting for signals...")

	for {
		select {
		case sig, ok := <-bridgeServer.SignalChan():
			if !ok {
				logger.Info("signal channel closed, shutting down")
				goto shutdown
			}

			metrics.SignalsReceived.Add(1)
			eventWriter.WriteSignal(sig)

			// Check if pipeline already has an active position.
			if pipeline.HasActivePosition() {
				logger.Warn("signal ignored — position already active",
					"signal_id", sig.SignalID,
				)
				metrics.SignalsRejected.Add(1)
				continue
			}

			// Process signal through the full pipeline.
			if err := pipeline.ProcessSignal(ctx, sig); err != nil {
				logger.Warn("signal processing failed",
					"signal_id", sig.SignalID,
					"error", err,
				)
				metrics.SignalsRejected.Add(1)
				continue
			}

			metrics.SignalsAccepted.Add(1)
			metrics.OrdersPlaced.Add(1)
			logger.Info("signal processed successfully",
				"signal_id", sig.SignalID,
			)

		case s := <-osSigCh:
			logger.Info("received OS signal, shutting down", "signal", s.String())
			goto shutdown
		}
	}

shutdown:
	cancel()
	bridgeServer.Stop()
	eventWriter.Stop()
	_ = exchange.Close()
	logger.Info("scalper server stopped")
}

func printBanner(cfg *config.Config) {
	mode := "LIVE"
	if cfg.BingX.PaperMode {
		mode = "PAPER"
	}
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║           BTC SCALPER — EXECUTION SERVER        ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf("║  Mode:      %-36s ║\n", mode)
	fmt.Printf("║  Bridge:    %s:%d                    ║\n", cfg.Bridge.Host, cfg.Bridge.Port)
	fmt.Printf("║  Symbol:    %-36s ║\n", cfg.BingX.Symbol)
	fmt.Printf("║  Risk:      %.1f%% base / %.1f%% escalated          ║\n", cfg.Risk.BaseRiskPercent, cfg.Risk.EscalatedRiskPercent)
	fmt.Printf("║  Max daily: %d trades / %d stop-losses            ║\n", cfg.Risk.MaxDailyTrades, cfg.Risk.MaxDailyStopLosses)
	fmt.Printf("║  Orders:    %-36s ║\n", cfg.Orders.OrderType)
	fmt.Printf("║  Metrics:   :%d/metrics                      ║\n", cfg.Monitoring.PrometheusPort)
	fmt.Printf("║  Log level: %-36s ║\n", cfg.Logging.Level)
	fmt.Println("╚══════════════════════════════════════════════════╝")
}
