package scaler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/oulman/tfc-agent-autoscaler/internal/ecs"
	"github.com/oulman/tfc-agent-autoscaler/internal/tfc"
)

type mockTFC struct {
	agentPoolStatusFn func(ctx context.Context) (busy, idle, total int, err error)
	pendingRunsFn     func(ctx context.Context) (int, error)
	agentDetailsFn    func(ctx context.Context) ([]tfc.AgentInfo, error)
}

func (m *mockTFC) GetAgentPoolStatus(ctx context.Context) (int, int, int, error) {
	return m.agentPoolStatusFn(ctx)
}

func (m *mockTFC) GetPendingRuns(ctx context.Context) (int, error) {
	return m.pendingRunsFn(ctx)
}

func (m *mockTFC) GetAgentDetails(ctx context.Context) ([]tfc.AgentInfo, error) {
	if m.agentDetailsFn != nil {
		return m.agentDetailsFn(ctx)
	}
	return nil, nil
}

type mockECS struct {
	serviceStatusFn    func(ctx context.Context) (int32, int32, error)
	setDesiredFn       func(ctx context.Context, count int32) error
	getTaskIPsFn       func(ctx context.Context) ([]ecs.TaskInfo, error)
	setTaskProtFn      func(ctx context.Context, taskArns []string, enabled bool, expiresInMinutes int32) error
	lastDesiredCount   int32
	protectCalls       []protectCall
}

type protectCall struct {
	taskArns         []string
	enabled          bool
	expiresInMinutes int32
}

func (m *mockECS) GetServiceStatus(ctx context.Context) (int32, int32, error) {
	return m.serviceStatusFn(ctx)
}

func (m *mockECS) SetDesiredCount(ctx context.Context, count int32) error {
	m.lastDesiredCount = count
	return m.setDesiredFn(ctx, count)
}

func (m *mockECS) GetTaskIPs(ctx context.Context) ([]ecs.TaskInfo, error) {
	if m.getTaskIPsFn != nil {
		return m.getTaskIPsFn(ctx)
	}
	return nil, nil
}

func (m *mockECS) SetTaskProtection(ctx context.Context, taskArns []string, enabled bool, expiresInMinutes int32) error {
	m.protectCalls = append(m.protectCalls, protectCall{taskArns: taskArns, enabled: enabled, expiresInMinutes: expiresInMinutes})
	if m.setTaskProtFn != nil {
		return m.setTaskProtFn(ctx, taskArns, enabled, expiresInMinutes)
	}
	return nil
}

func TestComputeDesired(t *testing.T) {
	tests := []struct {
		name        string
		pendingRuns int
		busyAgents  int
		minAgents   int
		maxAgents   int
		want        int
	}{
		{
			name:        "basic scale up",
			pendingRuns: 3,
			busyAgents:  2,
			minAgents:   0,
			maxAgents:   10,
			want:        5,
		},
		{
			name:        "clamped to max",
			pendingRuns: 20,
			busyAgents:  5,
			minAgents:   0,
			maxAgents:   10,
			want:        10,
		},
		{
			name:        "respects min",
			pendingRuns: 0,
			busyAgents:  0,
			minAgents:   2,
			maxAgents:   10,
			want:        2,
		},
		{
			name:        "no work no min",
			pendingRuns: 0,
			busyAgents:  0,
			minAgents:   0,
			maxAgents:   10,
			want:        0,
		},
		{
			name:        "busy agents only no pending",
			pendingRuns: 0,
			busyAgents:  3,
			minAgents:   0,
			maxAgents:   10,
			want:        3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeDesired(tt.pendingRuns, tt.busyAgents, tt.minAgents, tt.maxAgents)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name           string
		pendingRuns    int
		busyAgents     int
		idleAgents     int
		totalAgents    int
		currentDesired int32
		currentRunning int32
		minAgents      int
		maxAgents      int
		lastScaleTime  time.Time
		cooldown       time.Duration
		wantScale      bool
		wantCount      int32
	}{
		{
			name:           "scale up from zero",
			pendingRuns:    3,
			busyAgents:     0,
			currentDesired: 0,
			currentRunning: 0,
			minAgents:      0,
			maxAgents:      10,
			cooldown:       60 * time.Second,
			wantScale:      true,
			wantCount:      3,
		},
		{
			name:           "scale down with no work",
			pendingRuns:    0,
			busyAgents:     0,
			idleAgents:     5,
			totalAgents:    5,
			currentDesired: 5,
			currentRunning: 5,
			minAgents:      0,
			maxAgents:      10,
			cooldown:       60 * time.Second,
			wantScale:      true,
			wantCount:      0,
		},
		{
			name:           "no change needed",
			pendingRuns:    0,
			busyAgents:     3,
			idleAgents:     0,
			totalAgents:    3,
			currentDesired: 3,
			currentRunning: 3,
			minAgents:      0,
			maxAgents:      10,
			cooldown:       60 * time.Second,
			wantScale:      false,
		},
		{
			name:           "scale down blocked by cooldown",
			pendingRuns:    0,
			busyAgents:     0,
			idleAgents:     5,
			totalAgents:    5,
			currentDesired: 5,
			currentRunning: 5,
			minAgents:      0,
			maxAgents:      10,
			lastScaleTime:  time.Now(), // just scaled
			cooldown:       60 * time.Second,
			wantScale:      false,
		},
		{
			name:           "scale up ignores cooldown",
			pendingRuns:    5,
			busyAgents:     3,
			currentDesired: 3,
			currentRunning: 3,
			minAgents:      0,
			maxAgents:      10,
			lastScaleTime:  time.Now(), // just scaled
			cooldown:       60 * time.Second,
			wantScale:      true,
			wantCount:      8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ecsClient := &mockECS{
				serviceStatusFn: func(_ context.Context) (int32, int32, error) {
					return tt.currentDesired, tt.currentRunning, nil
				},
				setDesiredFn: func(_ context.Context, _ int32) error {
					return nil
				},
			}

			s := &Scaler{
				tfc: &mockTFC{
					agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
						return tt.busyAgents, tt.idleAgents, tt.totalAgents, nil
					},
					pendingRunsFn: func(_ context.Context) (int, error) {
						return tt.pendingRuns, nil
					},
				},
				ecs:           ecsClient,
				minAgents:     tt.minAgents,
				maxAgents:     tt.maxAgents,
				cooldown:      tt.cooldown,
				lastScaleTime: tt.lastScaleTime,
				logger:        slog.Default(),
			}

			err := s.Reconcile(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantScale {
				if ecsClient.lastDesiredCount != tt.wantCount {
					t.Errorf("scaled to %d, want %d", ecsClient.lastDesiredCount, tt.wantCount)
				}
			}
		})
	}
}

func TestReconcileDoesNotSignalReady(t *testing.T) {
	s := New(
		&mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		&mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		0, 10, time.Second, time.Minute, slog.Default(),
	)

	if err := s.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reconcile should NOT signal readiness; that's Run's job.
	select {
	case <-s.Ready():
		t.Fatal("Ready channel should not be closed by Reconcile")
	default:
	}
}

func TestRunSignalsReadyAfterFirstSuccess(t *testing.T) {
	s := New(
		&mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		&mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		0, 10, 50*time.Millisecond, time.Minute, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	select {
	case <-s.Ready():
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Ready channel was not closed after successful reconcile in Run")
	}
	cancel()
}

func TestRunDoesNotSignalReadyOnPersistentError(t *testing.T) {
	s := New(
		&mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, errors.New("fail")
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		&mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		0, 10, 50*time.Millisecond, time.Minute, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	// Give a few poll cycles for it to try.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-s.Ready():
		t.Fatal("Ready channel should not be closed when all reconciles fail")
	default:
	}
}

func TestReadyChannelIsIdempotent(t *testing.T) {
	s := New(
		&mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		&mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		0, 10, 50*time.Millisecond, time.Minute, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	<-s.Ready()

	// Multiple reads from a closed channel should all succeed.
	select {
	case <-s.Ready():
		// expected
	default:
		t.Fatal("Ready channel should remain closed (idempotent)")
	}
	cancel()
}

func TestReadyConcurrentAccess(t *testing.T) {
	s := New(
		&mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		&mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		0, 10, 50*time.Millisecond, time.Minute, slog.Default(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	// Multiple goroutines waiting on Ready should all unblock.
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-s.Ready()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all goroutines unblocked
	case <-time.After(2 * time.Second):
		t.Fatal("not all goroutines unblocked from Ready channel")
	}
	cancel()
}

type fakeMetrics struct {
	reconcileCalls        int
	lastBusy              int
	lastIdle              int
	lastTotal             int
	lastPending           int
	lastDesired           int
	lastRunning           int
	resultCalls           int
	lastSuccess           bool
	scaleEvents           []string
	cooldownSkips         int
	taskProtectionErrors  int
}

func (f *fakeMetrics) RecordReconcile(busy, idle, total, pending, desired, running int) {
	f.reconcileCalls++
	f.lastBusy = busy
	f.lastIdle = idle
	f.lastTotal = total
	f.lastPending = pending
	f.lastDesired = desired
	f.lastRunning = running
}

func (f *fakeMetrics) RecordReconcileResult(success bool) {
	f.resultCalls++
	f.lastSuccess = success
}

func (f *fakeMetrics) RecordScaleEvent(direction string) {
	f.scaleEvents = append(f.scaleEvents, direction)
}

func (f *fakeMetrics) RecordCooldownSkip() {
	f.cooldownSkips++
}

func (f *fakeMetrics) RecordTaskProtectionError() {
	f.taskProtectionErrors++
}

func TestReconcileRecordsMetrics(t *testing.T) {
	fm := &fakeMetrics{}
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 1, 1, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 2, 1, 3, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 4, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
		metrics:   fm,
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fm.reconcileCalls != 1 {
		t.Errorf("RecordReconcile called %d times, want 1", fm.reconcileCalls)
	}
	if fm.lastBusy != 2 || fm.lastIdle != 1 || fm.lastTotal != 3 || fm.lastPending != 4 {
		t.Errorf("gauge values: busy=%d idle=%d total=%d pending=%d", fm.lastBusy, fm.lastIdle, fm.lastTotal, fm.lastPending)
	}
	if fm.lastDesired != 1 || fm.lastRunning != 1 {
		t.Errorf("ecs values: desired=%d running=%d", fm.lastDesired, fm.lastRunning)
	}
	if !fm.lastSuccess {
		t.Error("expected success result")
	}
	// desired=6 vs current=1 → scale up
	if len(fm.scaleEvents) != 1 || fm.scaleEvents[0] != "up" {
		t.Errorf("scale events = %v, want [up]", fm.scaleEvents)
	}
}

func TestReconcileCooldownSkipRecordsMetric(t *testing.T) {
	fm := &fakeMetrics{}
	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 5, 5, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs: &mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 5, 5, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		minAgents:     0,
		maxAgents:     10,
		cooldown:      time.Minute,
		lastScaleTime: time.Now(),
		logger:        slog.Default(),
		metrics:       fm,
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fm.cooldownSkips != 1 {
		t.Errorf("cooldown skips = %d, want 1", fm.cooldownSkips)
	}
}

func TestReconcileErrorRecordsMetric(t *testing.T) {
	fm := &fakeMetrics{}
	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, errors.New("fail")
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs: &mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		logger:  slog.Default(),
		metrics: fm,
	}

	err := s.Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if fm.lastSuccess {
		t.Error("expected error result")
	}
}

func TestReconcileWithNilMetrics(t *testing.T) {
	// Ensure nil metrics doesn't panic
	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs: &mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		logger: slog.Default(),
		// metrics is nil
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcileTFCError(t *testing.T) {
	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 0, 0, errors.New("TFC API down")
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs: &mockECS{
			serviceStatusFn: func(_ context.Context) (int32, int32, error) {
				return 0, 0, nil
			},
			setDesiredFn: func(_ context.Context, _ int32) error {
				return nil
			},
		},
		logger: slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestReconcileScaleDownCappedByIdleCount(t *testing.T) {
	// 3 busy + 2 idle = 5 total, desired computes to 3 (busy only),
	// but idle guard caps scale-down to removing only 2 idle agents.
	// currentDesired=5, computedDesired=3, idle=2 → scaleDownBy=min(2,2)=2 → newDesired=3
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 5, 5, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
		getTaskIPsFn: func(_ context.Context) ([]ecs.TaskInfo, error) {
			return []ecs.TaskInfo{
				{TaskArn: "arn:task/1", PrivateIP: "10.0.0.1"},
				{TaskArn: "arn:task/2", PrivateIP: "10.0.0.2"},
				{TaskArn: "arn:task/3", PrivateIP: "10.0.0.3"},
				{TaskArn: "arn:task/4", PrivateIP: "10.0.0.4"},
				{TaskArn: "arn:task/5", PrivateIP: "10.0.0.5"},
			}, nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 3, 2, 5, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
			agentDetailsFn: func(_ context.Context) ([]tfc.AgentInfo, error) {
				return []tfc.AgentInfo{
					{ID: "a1", IP: "10.0.0.1", Status: "busy"},
					{ID: "a2", IP: "10.0.0.2", Status: "busy"},
					{ID: "a3", IP: "10.0.0.3", Status: "busy"},
					{ID: "a4", IP: "10.0.0.4", Status: "idle"},
					{ID: "a5", IP: "10.0.0.5", Status: "idle"},
				}, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ecsClient.lastDesiredCount != 3 {
		t.Errorf("scaled to %d, want 3", ecsClient.lastDesiredCount)
	}
}

func TestReconcileScaleDownCappedWhenMoreBusyThanComputed(t *testing.T) {
	// currentDesired=5, computedDesired=0 (no work), but 3 busy + 2 idle
	// idle guard: scaleDownBy=min(5-0, 2)=2 → newDesired=3
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 5, 5, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
		getTaskIPsFn: func(_ context.Context) ([]ecs.TaskInfo, error) {
			return []ecs.TaskInfo{
				{TaskArn: "arn:task/1", PrivateIP: "10.0.0.1"},
				{TaskArn: "arn:task/2", PrivateIP: "10.0.0.2"},
				{TaskArn: "arn:task/3", PrivateIP: "10.0.0.3"},
				{TaskArn: "arn:task/4", PrivateIP: "10.0.0.4"},
				{TaskArn: "arn:task/5", PrivateIP: "10.0.0.5"},
			}, nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 3, 2, 5, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
			agentDetailsFn: func(_ context.Context) ([]tfc.AgentInfo, error) {
				return []tfc.AgentInfo{
					{ID: "a1", IP: "10.0.0.1", Status: "busy"},
					{ID: "a2", IP: "10.0.0.2", Status: "busy"},
					{ID: "a3", IP: "10.0.0.3", Status: "busy"},
					{ID: "a4", IP: "10.0.0.4", Status: "idle"},
					{ID: "a5", IP: "10.0.0.5", Status: "idle"},
				}, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ecsClient.lastDesiredCount != 3 {
		t.Errorf("scaled to %d, want 3", ecsClient.lastDesiredCount)
	}
}

func TestReconcileAllBusyNoScaleDown(t *testing.T) {
	// All agents busy, idle=0, computedDesired=3 == currentDesired=3 → no change
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 3, 3, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			t.Fatal("SetDesiredCount should not be called when no change needed")
			return nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 3, 0, 3, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcileBusyTasksGetProtected(t *testing.T) {
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 5, 5, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
		getTaskIPsFn: func(_ context.Context) ([]ecs.TaskInfo, error) {
			return []ecs.TaskInfo{
				{TaskArn: "arn:task/1", PrivateIP: "10.0.0.1"},
				{TaskArn: "arn:task/2", PrivateIP: "10.0.0.2"},
				{TaskArn: "arn:task/3", PrivateIP: "10.0.0.3"},
			}, nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 2, 1, 3, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
			agentDetailsFn: func(_ context.Context) ([]tfc.AgentInfo, error) {
				return []tfc.AgentInfo{
					{ID: "a1", IP: "10.0.0.1", Status: "busy"},
					{ID: "a2", IP: "10.0.0.2", Status: "busy"},
					{ID: "a3", IP: "10.0.0.3", Status: "idle"},
				}, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 protection calls: one for busy (enable), one for idle (disable)
	if len(ecsClient.protectCalls) < 2 {
		t.Fatalf("expected at least 2 protection calls, got %d", len(ecsClient.protectCalls))
	}

	// Find the enable and disable calls
	var enableCall, disableCall *protectCall
	for i := range ecsClient.protectCalls {
		if ecsClient.protectCalls[i].enabled {
			enableCall = &ecsClient.protectCalls[i]
		} else {
			disableCall = &ecsClient.protectCalls[i]
		}
	}

	if enableCall == nil {
		t.Fatal("expected a protect-enable call for busy tasks")
	}
	if len(enableCall.taskArns) != 2 {
		t.Errorf("expected 2 busy task ARNs, got %d", len(enableCall.taskArns))
	}
	if enableCall.expiresInMinutes != 120 {
		t.Errorf("expected expiresInMinutes=120, got %d", enableCall.expiresInMinutes)
	}

	if disableCall == nil {
		t.Fatal("expected a protect-disable call for idle tasks")
	}
	if len(disableCall.taskArns) != 1 {
		t.Errorf("expected 1 idle task ARN, got %d", len(disableCall.taskArns))
	}
}

func TestReconcileProtectionFailureIsNonFatal(t *testing.T) {
	fm := &fakeMetrics{}
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 5, 5, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
		getTaskIPsFn: func(_ context.Context) ([]ecs.TaskInfo, error) {
			return nil, errors.New("task IP lookup failed")
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 0, 5, 5, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
			agentDetailsFn: func(_ context.Context) ([]tfc.AgentInfo, error) {
				return []tfc.AgentInfo{
					{ID: "a1", IP: "10.0.0.1", Status: "idle"},
				}, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
		metrics:   fm,
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("expected no error (protection failure is non-fatal), got: %v", err)
	}

	// Should still scale down (idle-guarded)
	if ecsClient.lastDesiredCount != 0 {
		t.Errorf("scaled to %d, want 0", ecsClient.lastDesiredCount)
	}

	if fm.taskProtectionErrors != 1 {
		t.Errorf("task protection errors = %d, want 1", fm.taskProtectionErrors)
	}
}

func TestReconcileNoProtectionCallsOnScaleUp(t *testing.T) {
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 2, 2, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 2, 0, 2, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 5, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ecsClient.protectCalls) != 0 {
		t.Errorf("expected no protection calls on scale-up, got %d", len(ecsClient.protectCalls))
	}
}

func TestReconcileNoProtectionCallsOnNoChange(t *testing.T) {
	ecsClient := &mockECS{
		serviceStatusFn: func(_ context.Context) (int32, int32, error) {
			return 3, 3, nil
		},
		setDesiredFn: func(_ context.Context, _ int32) error {
			return nil
		},
	}

	s := &Scaler{
		tfc: &mockTFC{
			agentPoolStatusFn: func(_ context.Context) (int, int, int, error) {
				return 3, 0, 3, nil
			},
			pendingRunsFn: func(_ context.Context) (int, error) {
				return 0, nil
			},
		},
		ecs:       ecsClient,
		minAgents: 0,
		maxAgents: 10,
		cooldown:  time.Minute,
		logger:    slog.Default(),
	}

	err := s.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ecsClient.protectCalls) != 0 {
		t.Errorf("expected no protection calls when no change, got %d", len(ecsClient.protectCalls))
	}
}
