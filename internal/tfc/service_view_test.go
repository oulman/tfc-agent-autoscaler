package tfc

import (
	"context"
	"fmt"
	"testing"
)

func TestServiceViewGetPendingRuns(t *testing.T) {
	tests := []struct {
		name     string
		runType  RunType
		counts   PendingRunCounts
		wantRuns int
	}{
		{
			name:     "plan type returns plan pending",
			runType:  RunTypePlan,
			counts:   PendingRunCounts{PlanPending: 5, ApplyPending: 3},
			wantRuns: 5,
		},
		{
			name:     "apply type returns apply pending",
			runType:  RunTypeApply,
			counts:   PendingRunCounts{PlanPending: 5, ApplyPending: 3},
			wantRuns: 3,
		},
		{
			name:     "zero counts",
			runType:  RunTypePlan,
			counts:   PendingRunCounts{PlanPending: 0, ApplyPending: 0},
			wantRuns: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := NewServiceView(&mockServiceViewClient{
				pendingRunsByTypeFn: func(_ context.Context) (PendingRunCounts, error) {
					return tt.counts, nil
				},
			}, tt.runType, nil)

			got, err := sv.GetPendingRuns(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantRuns {
				t.Errorf("got %d, want %d", got, tt.wantRuns)
			}
		})
	}
}

func TestServiceViewGetAgentPoolStatus(t *testing.T) {
	allAgents := []AgentInfo{
		{ID: "a1", IP: "10.0.0.1", Status: "busy"},
		{ID: "a2", IP: "10.0.0.2", Status: "idle"},
		{ID: "a3", IP: "10.0.0.3", Status: "busy"},
		{ID: "a4", IP: "10.0.0.4", Status: "idle"},
	}

	// Only 10.0.0.1 and 10.0.0.3 belong to this service's tasks.
	taskIPs := map[string]bool{
		"10.0.0.1": true,
		"10.0.0.3": true,
	}

	sv := NewServiceView(&mockServiceViewClient{
		agentDetailsFn: func(_ context.Context) ([]AgentInfo, error) {
			return allAgents, nil
		},
	}, RunTypePlan, func(_ context.Context) (map[string]bool, error) {
		return taskIPs, nil
	})

	busy, idle, total, err := sv.GetAgentPoolStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if busy != 2 {
		t.Errorf("busy: got %d, want 2", busy)
	}
	if idle != 0 {
		t.Errorf("idle: got %d, want 0", idle)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
}

func TestServiceViewGetAgentPoolStatusAllIdle(t *testing.T) {
	allAgents := []AgentInfo{
		{ID: "a1", IP: "10.0.0.1", Status: "idle"},
		{ID: "a2", IP: "10.0.0.2", Status: "idle"},
	}

	taskIPs := map[string]bool{
		"10.0.0.1": true,
		"10.0.0.2": true,
	}

	sv := NewServiceView(&mockServiceViewClient{
		agentDetailsFn: func(_ context.Context) ([]AgentInfo, error) {
			return allAgents, nil
		},
	}, RunTypeApply, func(_ context.Context) (map[string]bool, error) {
		return taskIPs, nil
	})

	busy, idle, total, err := sv.GetAgentPoolStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if busy != 0 {
		t.Errorf("busy: got %d, want 0", busy)
	}
	if idle != 2 {
		t.Errorf("idle: got %d, want 2", idle)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
}

func TestServiceViewGetAgentDetails(t *testing.T) {
	allAgents := []AgentInfo{
		{ID: "a1", IP: "10.0.0.1", Status: "busy"},
		{ID: "a2", IP: "10.0.0.2", Status: "idle"},
		{ID: "a3", IP: "10.0.0.3", Status: "busy"},
	}

	taskIPs := map[string]bool{
		"10.0.0.1": true,
		"10.0.0.3": true,
	}

	sv := NewServiceView(&mockServiceViewClient{
		agentDetailsFn: func(_ context.Context) ([]AgentInfo, error) {
			return allAgents, nil
		},
	}, RunTypePlan, func(_ context.Context) (map[string]bool, error) {
		return taskIPs, nil
	})

	agents, err := sv.GetAgentDetails(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	if agents[0].ID != "a1" || agents[1].ID != "a3" {
		t.Errorf("got agents %+v, want a1 and a3", agents)
	}
}

func TestServiceViewGetPendingRunsError(t *testing.T) {
	sv := NewServiceView(&mockServiceViewClient{
		pendingRunsByTypeFn: func(_ context.Context) (PendingRunCounts, error) {
			return PendingRunCounts{}, fmt.Errorf("api error")
		},
	}, RunTypePlan, nil)

	_, err := sv.GetPendingRuns(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestServiceViewGetAgentPoolStatusNoMatchingIPs(t *testing.T) {
	allAgents := []AgentInfo{
		{ID: "a1", IP: "10.0.0.1", Status: "busy"},
	}

	sv := NewServiceView(&mockServiceViewClient{
		agentDetailsFn: func(_ context.Context) ([]AgentInfo, error) {
			return allAgents, nil
		},
	}, RunTypePlan, func(_ context.Context) (map[string]bool, error) {
		return map[string]bool{"10.0.0.99": true}, nil
	})

	busy, idle, total, err := sv.GetAgentPoolStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if busy != 0 || idle != 0 || total != 0 {
		t.Errorf("expected all zeros, got busy=%d idle=%d total=%d", busy, idle, total)
	}
}

// mockServiceViewClient is used by ServiceView tests to mock the underlying Client methods.
type mockServiceViewClient struct {
	agentDetailsFn      func(ctx context.Context) ([]AgentInfo, error)
	pendingRunsByTypeFn func(ctx context.Context) (PendingRunCounts, error)
}

func (m *mockServiceViewClient) GetAgentDetails(ctx context.Context) ([]AgentInfo, error) {
	return m.agentDetailsFn(ctx)
}

func (m *mockServiceViewClient) GetPendingRunsByType(ctx context.Context) (PendingRunCounts, error) {
	return m.pendingRunsByTypeFn(ctx)
}
