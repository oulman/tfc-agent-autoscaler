package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/oulman/tfc-agent-autoscaler/internal/config"
	"github.com/oulman/tfc-agent-autoscaler/internal/ecs"
	"github.com/oulman/tfc-agent-autoscaler/internal/health"
	"github.com/oulman/tfc-agent-autoscaler/internal/metrics"
	"github.com/oulman/tfc-agent-autoscaler/internal/scaler"
	"github.com/oulman/tfc-agent-autoscaler/internal/tfc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	tfcClient, err := tfc.New(cfg.TFCToken, cfg.TFCAddress, cfg.TFCAgentPoolID)
	if err != nil {
		logger.Error("failed to create TFC client", "error", err)
		os.Exit(1)
	}

	ecsClient, err := ecs.New(ctx, cfg.ECSCluster, cfg.ECSService)
	if err != nil {
		logger.Error("failed to create ECS client", "error", err)
		os.Exit(1)
	}

	m := metrics.New()

	s := scaler.New(
		tfcClient,
		ecsClient,
		cfg.MinAgents,
		cfg.MaxAgents,
		cfg.PollInterval,
		cfg.CooldownPeriod,
		logger,
	)
	s.SetMetrics(m)

	healthSrv := health.NewServer(cfg.HealthAddr, health.NewChannelProbe(s.Ready()), health.WithMetricsHandler(m.Handler()))
	go func() {
		if err := healthSrv.Run(ctx); err != nil {
			logger.Error("health server error", "error", err)
		}
	}()

	if err := s.Run(ctx); err != nil && ctx.Err() != nil {
		logger.Info("autoscaler stopped", "reason", err)
	}
}
