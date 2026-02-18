package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
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

	m := metrics.New()

	if cfg.SpotService != nil {
		runDualService(ctx, logger, cfg, tfcClient, m)
	} else {
		runSingleService(ctx, logger, cfg, tfcClient, m)
	}
}

func runSingleService(ctx context.Context, logger *slog.Logger, cfg config.Config, tfcClient *tfc.Client, m *metrics.Metrics) {
	ecsClient, err := ecs.New(ctx, cfg.ECSCluster, cfg.ECSService)
	if err != nil {
		logger.Error("failed to create ECS client", "error", err)
		os.Exit(1)
	}

	s := scaler.New("default",
		tfcClient,
		ecsClient,
		cfg.MinAgents,
		cfg.MaxAgents,
		cfg.PollInterval,
		cfg.CooldownPeriod,
		logger,
	)
	s.SetMetrics(m.ForService("default"))

	healthSrv := health.NewServer(cfg.HealthAddr, health.NewChannelProbe(s.Ready()), health.WithMetricsHandler(m.Handler()))
	go func() {
		if err := healthSrv.Run(ctx); err != nil {
			logger.Error("health server error", "error", err)
		}
	}()

	if err := s.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("autoscaler stopped", "reason", err)
		} else {
			logger.Error("autoscaler failed", "error", err)
		}
	}
}

func runDualService(ctx context.Context, logger *slog.Logger, cfg config.Config, tfcClient *tfc.Client, m *metrics.Metrics) {
	regularECS, err := ecs.New(ctx, cfg.ECSCluster, cfg.ECSService)
	if err != nil {
		logger.Error("failed to create regular ECS client", "error", err)
		os.Exit(1)
	}

	spotECS, err := ecs.New(ctx, cfg.ECSCluster, cfg.SpotService.ECSService)
	if err != nil {
		logger.Error("failed to create spot ECS client", "error", err)
		os.Exit(1)
	}

	regularView := tfc.NewServiceView(tfcClient, tfc.RunTypeApply, taskIPsFetcher(regularECS))
	spotView := tfc.NewServiceView(tfcClient, tfc.RunTypePlan, taskIPsFetcher(spotECS))

	regularScaler := scaler.New("regular",
		regularView,
		regularECS,
		cfg.MinAgents,
		cfg.MaxAgents,
		cfg.PollInterval,
		cfg.CooldownPeriod,
		logger,
	)
	regularScaler.SetMetrics(m.ForService("regular"))

	spotScaler := scaler.New("spot",
		spotView,
		spotECS,
		cfg.SpotService.MinAgents,
		cfg.SpotService.MaxAgents,
		cfg.PollInterval,
		cfg.CooldownPeriod,
		logger,
	)
	spotScaler.SetMetrics(m.ForService("spot"))

	probe := health.NewCompositeProbe(
		health.NewChannelProbe(regularScaler.Ready()),
		health.NewChannelProbe(spotScaler.Ready()),
	)

	healthSrv := health.NewServer(cfg.HealthAddr, probe, health.WithMetricsHandler(m.Handler()))
	go func() {
		if err := healthSrv.Run(ctx); err != nil {
			logger.Error("health server error", "error", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := regularScaler.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				logger.Info("regular scaler stopped", "reason", err)
			} else {
				logger.Error("regular scaler failed", "error", err)
			}
		}
	}()

	go func() {
		defer wg.Done()
		if err := spotScaler.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				logger.Info("spot scaler stopped", "reason", err)
			} else {
				logger.Error("spot scaler failed", "error", err)
			}
		}
	}()

	wg.Wait()
}

func taskIPsFetcher(ecsClient *ecs.Client) tfc.TaskIPsFunc {
	return func(ctx context.Context) (map[string]bool, error) {
		tasks, err := ecsClient.GetTaskIPs(ctx)
		if err != nil {
			return nil, err
		}
		ips := make(map[string]bool, len(tasks))
		for _, t := range tasks {
			if t.PrivateIP != "" {
				ips[t.PrivateIP] = true
			}
		}
		return ips, nil
	}
}
