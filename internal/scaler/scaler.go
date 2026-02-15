// Package scaler implements the autoscaling decision engine.
package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oulman/tfc-agent-autoscaler/internal/ecs"
	"github.com/oulman/tfc-agent-autoscaler/internal/tfc"
)

// TFCClient is the interface for querying Terraform Cloud state.
type TFCClient interface {
	GetAgentPoolStatus(ctx context.Context) (busy, idle, total int, err error)
	GetPendingRuns(ctx context.Context) (int, error)
	GetAgentDetails(ctx context.Context) ([]tfc.AgentInfo, error)
}

// ECSClient is the interface for managing the ECS service.
type ECSClient interface {
	GetServiceStatus(ctx context.Context) (desired, running int32, err error)
	SetDesiredCount(ctx context.Context, count int32) error
	GetTaskIPs(ctx context.Context) ([]ecs.TaskInfo, error)
	SetTaskProtection(ctx context.Context, taskArns []string, enabled bool, expiresInMinutes int32) error
}

// MetricsRecorder records autoscaler metrics.
type MetricsRecorder interface {
	RecordReconcile(busy, idle, total, pending, desired, running int)
	RecordReconcileResult(success bool)
	RecordScaleEvent(direction string)
	RecordCooldownSkip()
	RecordTaskProtectionError()
}

// Scaler orchestrates the autoscaling control loop.
type Scaler struct {
	tfc           TFCClient
	ecs           ECSClient
	minAgents     int
	maxAgents     int
	pollInterval  time.Duration
	cooldown      time.Duration
	lastScaleTime time.Time
	logger        *slog.Logger
	ready         chan struct{}
	readyOnce     sync.Once
	metrics       MetricsRecorder
}

// New creates a new Scaler.
func New(tfc TFCClient, ecs ECSClient, minAgents, maxAgents int, pollInterval, cooldown time.Duration, logger *slog.Logger) *Scaler {
	return &Scaler{
		tfc:          tfc,
		ecs:          ecs,
		minAgents:    minAgents,
		maxAgents:    maxAgents,
		pollInterval: pollInterval,
		cooldown:     cooldown,
		logger:       logger,
		ready:        make(chan struct{}),
	}
}

// SetMetrics configures an optional metrics recorder.
func (s *Scaler) SetMetrics(m MetricsRecorder) {
	s.metrics = m
}

// Ready returns a channel that is closed after the first successful reconcile.
func (s *Scaler) Ready() <-chan struct{} {
	return s.ready
}

// Run starts the polling loop and blocks until the context is canceled.
func (s *Scaler) Run(ctx context.Context) error {
	s.logger.Info("starting autoscaler",
		"min_agents", s.minAgents,
		"max_agents", s.maxAgents,
		"poll_interval", s.pollInterval,
		"cooldown", s.cooldown,
	)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	// Run immediately on start, then on each tick.
	if err := s.Reconcile(ctx); err != nil {
		s.logger.Error("reconcile failed", "error", err)
	} else {
		s.markReady()
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("shutting down autoscaler")
			return ctx.Err()
		case <-ticker.C:
			if err := s.Reconcile(ctx); err != nil {
				s.logger.Error("reconcile failed", "error", err)
			} else {
				s.markReady()
			}
		}
	}
}

// Reconcile performs a single check-and-scale cycle.
func (s *Scaler) Reconcile(ctx context.Context) error {
	busy, idle, total, err := s.tfc.GetAgentPoolStatus(ctx)
	if err != nil {
		s.recordResult(false)
		return fmt.Errorf("getting agent pool status: %w", err)
	}

	pendingRuns, err := s.tfc.GetPendingRuns(ctx)
	if err != nil {
		s.recordResult(false)
		return fmt.Errorf("getting pending runs: %w", err)
	}

	currentDesired, currentRunning, err := s.ecs.GetServiceStatus(ctx)
	if err != nil {
		s.recordResult(false)
		return fmt.Errorf("getting ECS service status: %w", err)
	}

	if s.metrics != nil {
		s.metrics.RecordReconcile(busy, idle, total, pendingRuns, int(currentDesired), int(currentRunning))
	}

	desired := computeDesired(pendingRuns, busy, s.minAgents, s.maxAgents)
	desiredInt32 := int32(desired)

	s.logger.Info("reconcile",
		"pending_runs", pendingRuns,
		"busy_agents", busy,
		"idle_agents", idle,
		"total_agents", total,
		"current_desired", currentDesired,
		"current_running", currentRunning,
		"computed_desired", desired,
	)

	if desiredInt32 == currentDesired {
		s.recordResult(true)
		return nil
	}

	// Scale-up always proceeds immediately. Scale-down respects cooldown and idle guard.
	if desiredInt32 < currentDesired {
		if !s.lastScaleTime.IsZero() && time.Since(s.lastScaleTime) < s.cooldown {
			s.logger.Info("scale-down skipped due to cooldown",
				"last_scale", s.lastScaleTime,
				"cooldown_remaining", s.cooldown-time.Since(s.lastScaleTime),
			)
			if s.metrics != nil {
				s.metrics.RecordCooldownSkip()
			}
			s.recordResult(true)
			return nil
		}

		// Idle guard: never scale down by more than the number of idle agents.
		scaleDownBy := int(currentDesired) - desired
		if idle < scaleDownBy {
			scaleDownBy = idle
		}
		desiredInt32 = currentDesired - int32(scaleDownBy)

		s.logger.Info("idle guard applied",
			"computed_desired", desired,
			"idle_agents", idle,
			"scale_down_by", scaleDownBy,
			"guarded_desired", desiredInt32,
		)

		if desiredInt32 == currentDesired {
			s.recordResult(true)
			return nil
		}

		// Task protection: protect busy tasks before scaling down.
		if err := s.protectBusyTasks(ctx); err != nil {
			s.logger.Warn("task protection failed, proceeding with idle-guarded scale-down",
				"error", err,
			)
			if s.metrics != nil {
				s.metrics.RecordTaskProtectionError()
			}
		}
	}

	direction := "up"
	if desiredInt32 < currentDesired {
		direction = "down"
	}

	s.logger.Info("scaling",
		"from", currentDesired,
		"to", desiredInt32,
	)

	if err := s.ecs.SetDesiredCount(ctx, desiredInt32); err != nil {
		s.recordResult(false)
		return fmt.Errorf("setting desired count: %w", err)
	}

	if s.metrics != nil {
		s.metrics.RecordScaleEvent(direction)
	}

	s.lastScaleTime = time.Now()
	s.recordResult(true)
	return nil
}

// protectBusyTasks correlates TFC agents with ECS tasks by IP and sets
// scale-in protection on busy tasks while removing it from idle ones.
func (s *Scaler) protectBusyTasks(ctx context.Context) error {
	agents, err := s.tfc.GetAgentDetails(ctx)
	if err != nil {
		return fmt.Errorf("getting agent details: %w", err)
	}

	tasks, err := s.ecs.GetTaskIPs(ctx)
	if err != nil {
		return fmt.Errorf("getting task IPs: %w", err)
	}

	// Build IP → task ARN map.
	ipToArn := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if t.PrivateIP != "" {
			ipToArn[t.PrivateIP] = t.TaskArn
		}
	}

	var busyArns, idleArns []string
	for _, agent := range agents {
		arn, ok := ipToArn[agent.IP]
		if !ok {
			continue
		}
		if agent.Status == "busy" {
			busyArns = append(busyArns, arn)
		} else {
			idleArns = append(idleArns, arn)
		}
	}

	if len(busyArns) > 0 {
		if err := s.ecs.SetTaskProtection(ctx, busyArns, true, 120); err != nil {
			return fmt.Errorf("protecting busy tasks: %w", err)
		}
	}

	if len(idleArns) > 0 {
		if err := s.ecs.SetTaskProtection(ctx, idleArns, false, 0); err != nil {
			return fmt.Errorf("unprotecting idle tasks: %w", err)
		}
	}

	s.logger.Info("task protection updated",
		"busy_protected", len(busyArns),
		"idle_unprotected", len(idleArns),
	)

	return nil
}

func (s *Scaler) recordResult(success bool) {
	if s.metrics != nil {
		s.metrics.RecordReconcileResult(success)
	}
}

func (s *Scaler) markReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

// computeDesired calculates the target agent count.
// Formula: desired = max(min, min(pendingRuns + busyAgents, max))
func computeDesired(pendingRuns, busyAgents, minAgents, maxAgents int) int {
	desired := pendingRuns + busyAgents
	return max(minAgents, min(desired, maxAgents))
}
