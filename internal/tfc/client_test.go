package tfc

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/go-tfe"
)

// mockAgentPools implements the subset of tfe.AgentPools we use.
type mockAgentPools struct {
	readWithOptionsFn func(ctx context.Context, agentPoolID string, options *tfe.AgentPoolReadOptions) (*tfe.AgentPool, error)
}

func (m *mockAgentPools) ReadWithOptions(ctx context.Context, agentPoolID string, options *tfe.AgentPoolReadOptions) (*tfe.AgentPool, error) {
	return m.readWithOptionsFn(ctx, agentPoolID, options)
}

// mockAgents implements the subset of tfe.Agents we use.
type mockAgents struct {
	listFn func(ctx context.Context, agentPoolID string, options *tfe.AgentListOptions) (*tfe.AgentList, error)
}

func (m *mockAgents) List(ctx context.Context, agentPoolID string, options *tfe.AgentListOptions) (*tfe.AgentList, error) {
	return m.listFn(ctx, agentPoolID, options)
}

// mockRuns implements the subset of tfe.Runs we use.
type mockRuns struct {
	listFn func(ctx context.Context, workspaceID string, options *tfe.RunListOptions) (*tfe.RunList, error)
}

func (m *mockRuns) List(ctx context.Context, workspaceID string, options *tfe.RunListOptions) (*tfe.RunList, error) {
	return m.listFn(ctx, workspaceID, options)
}

func TestGetAgentPoolStatus(t *testing.T) {
	tests := []struct {
		name      string
		agents    []*tfe.Agent
		wantBusy  int
		wantIdle  int
		wantTotal int
		wantErr   bool
	}{
		{
			name: "mixed statuses",
			agents: []*tfe.Agent{
				{ID: "agent-1", Status: "idle"},
				{ID: "agent-2", Status: "busy"},
				{ID: "agent-3", Status: "busy"},
				{ID: "agent-4", Status: "idle"},
				{ID: "agent-5", Status: "unknown"},
			},
			wantBusy:  2,
			wantIdle:  2,
			wantTotal: 5,
		},
		{
			name:      "no agents",
			agents:    []*tfe.Agent{},
			wantBusy:  0,
			wantIdle:  0,
			wantTotal: 0,
		},
		{
			name: "all busy",
			agents: []*tfe.Agent{
				{ID: "agent-1", Status: "busy"},
				{ID: "agent-2", Status: "busy"},
			},
			wantBusy:  2,
			wantIdle:  0,
			wantTotal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				agentPoolID: "apool-123",
				agents: &mockAgents{
					listFn: func(_ context.Context, _ string, _ *tfe.AgentListOptions) (*tfe.AgentList, error) {
						return &tfe.AgentList{
							Items:      tt.agents,
							Pagination: &tfe.Pagination{TotalPages: 1, CurrentPage: 1},
						}, nil
					},
				},
			}

			busy, idle, total, err := c.GetAgentPoolStatus(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if busy != tt.wantBusy {
				t.Errorf("busy: got %d, want %d", busy, tt.wantBusy)
			}
			if idle != tt.wantIdle {
				t.Errorf("idle: got %d, want %d", idle, tt.wantIdle)
			}
			if total != tt.wantTotal {
				t.Errorf("total: got %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestGetAgentDetails(t *testing.T) {
	tests := []struct {
		name    string
		listFn  func(ctx context.Context, agentPoolID string, options *tfe.AgentListOptions) (*tfe.AgentList, error)
		want    []AgentInfo
		wantErr bool
	}{
		{
			name: "mixed agents",
			listFn: func(_ context.Context, _ string, _ *tfe.AgentListOptions) (*tfe.AgentList, error) {
				return &tfe.AgentList{
					Items: []*tfe.Agent{
						{ID: "agent-1", Name: "worker-1", IP: "10.0.0.1", Status: "idle"},
						{ID: "agent-2", Name: "worker-2", IP: "10.0.0.2", Status: "busy"},
						{ID: "agent-3", Name: "worker-3", IP: "10.0.0.3", Status: "unknown"},
					},
					Pagination: &tfe.Pagination{TotalPages: 1, CurrentPage: 1},
				}, nil
			},
			want: []AgentInfo{
				{ID: "agent-1", Name: "worker-1", IP: "10.0.0.1", Status: "idle"},
				{ID: "agent-2", Name: "worker-2", IP: "10.0.0.2", Status: "busy"},
				{ID: "agent-3", Name: "worker-3", IP: "10.0.0.3", Status: "unknown"},
			},
		},
		{
			name: "empty pool",
			listFn: func(_ context.Context, _ string, _ *tfe.AgentListOptions) (*tfe.AgentList, error) {
				return &tfe.AgentList{
					Items:      []*tfe.Agent{},
					Pagination: &tfe.Pagination{TotalPages: 1, CurrentPage: 1},
				}, nil
			},
			want: nil,
		},
		{
			name: "API error",
			listFn: func(_ context.Context, _ string, _ *tfe.AgentListOptions) (*tfe.AgentList, error) {
				return nil, errors.New("api failure")
			},
			wantErr: true,
		},
		{
			name: "multi-page pagination",
			listFn: func(_ context.Context, _ string, opts *tfe.AgentListOptions) (*tfe.AgentList, error) {
				if opts.PageNumber == 0 || opts.PageNumber == 1 {
					return &tfe.AgentList{
						Items: []*tfe.Agent{
							{ID: "agent-1", Name: "worker-1", IP: "10.0.0.1", Status: "idle"},
						},
						Pagination: &tfe.Pagination{TotalPages: 2, CurrentPage: 1, NextPage: 2},
					}, nil
				}
				return &tfe.AgentList{
					Items: []*tfe.Agent{
						{ID: "agent-2", Name: "worker-2", IP: "10.0.0.2", Status: "busy"},
					},
					Pagination: &tfe.Pagination{TotalPages: 2, CurrentPage: 2},
				}, nil
			},
			want: []AgentInfo{
				{ID: "agent-1", Name: "worker-1", IP: "10.0.0.1", Status: "idle"},
				{ID: "agent-2", Name: "worker-2", IP: "10.0.0.2", Status: "busy"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				agentPoolID: "apool-123",
				agents:      &mockAgents{listFn: tt.listFn},
			}

			got, err := c.GetAgentDetails(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d agents, want %d", len(got), len(tt.want))
			}
			for i, g := range got {
				w := tt.want[i]
				if g.ID != w.ID || g.Name != w.Name || g.IP != w.IP || g.Status != w.Status {
					t.Errorf("agent[%d]: got %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestGetPendingRunsByType(t *testing.T) {
	tests := []struct {
		name             string
		workspaces       []*tfe.Workspace
		runsPerStatus    map[string]map[string]int // wsID -> status filter -> count
		wantPlanPending  int
		wantApplyPending int
		wantErr          bool
	}{
		{
			name: "mixed plan and apply pending",
			workspaces: []*tfe.Workspace{
				{ID: "ws-1"},
			},
			runsPerStatus: map[string]map[string]int{
				"ws-1": {
					planPendingStatuses:  3,
					applyPendingStatuses: 2,
				},
			},
			wantPlanPending:  3,
			wantApplyPending: 2,
		},
		{
			name: "multiple workspaces",
			workspaces: []*tfe.Workspace{
				{ID: "ws-1"},
				{ID: "ws-2"},
			},
			runsPerStatus: map[string]map[string]int{
				"ws-1": {
					planPendingStatuses:  1,
					applyPendingStatuses: 2,
				},
				"ws-2": {
					planPendingStatuses:  4,
					applyPendingStatuses: 0,
				},
			},
			wantPlanPending:  5,
			wantApplyPending: 2,
		},
		{
			name:             "no workspaces",
			workspaces:       nil,
			runsPerStatus:    map[string]map[string]int{},
			wantPlanPending:  0,
			wantApplyPending: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				agentPoolID: "apool-123",
				agentPools: &mockAgentPools{
					readWithOptionsFn: func(_ context.Context, _ string, _ *tfe.AgentPoolReadOptions) (*tfe.AgentPool, error) {
						return &tfe.AgentPool{
							ID:         "apool-123",
							Workspaces: tt.workspaces,
						}, nil
					},
				},
				runs: &mockRuns{
					listFn: func(_ context.Context, wsID string, opts *tfe.RunListOptions) (*tfe.RunList, error) {
						statusCounts := tt.runsPerStatus[wsID]
						count := statusCounts[opts.Status]
						items := make([]*tfe.Run, count)
						for i := range items {
							items[i] = &tfe.Run{ID: "run-placeholder"}
						}
						return &tfe.RunList{
							Items:      items,
							Pagination: &tfe.Pagination{TotalCount: count, TotalPages: 1, CurrentPage: 1},
						}, nil
					},
				},
			}

			counts, err := c.GetPendingRunsByType(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if counts.PlanPending != tt.wantPlanPending {
				t.Errorf("PlanPending: got %d, want %d", counts.PlanPending, tt.wantPlanPending)
			}
			if counts.ApplyPending != tt.wantApplyPending {
				t.Errorf("ApplyPending: got %d, want %d", counts.ApplyPending, tt.wantApplyPending)
			}
		})
	}
}

func TestGetPendingRuns(t *testing.T) {
	tests := []struct {
		name       string
		workspaces []*tfe.Workspace
		runsPerWS  map[string]map[string]int // workspace ID -> status filter -> count
		wantCount  int
		wantErr    bool
	}{
		{
			name: "runs across multiple workspaces",
			workspaces: []*tfe.Workspace{
				{ID: "ws-1"},
				{ID: "ws-2"},
			},
			runsPerWS: map[string]map[string]int{
				"ws-1": {planPendingStatuses: 2, applyPendingStatuses: 1},
				"ws-2": {planPendingStatuses: 1, applyPendingStatuses: 1},
			},
			wantCount: 5,
		},
		{
			name:       "no workspaces",
			workspaces: nil,
			runsPerWS:  map[string]map[string]int{},
			wantCount:  0,
		},
		{
			name: "workspaces with no pending runs",
			workspaces: []*tfe.Workspace{
				{ID: "ws-1"},
			},
			runsPerWS: map[string]map[string]int{
				"ws-1": {planPendingStatuses: 0, applyPendingStatuses: 0},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				agentPoolID: "apool-123",
				agentPools: &mockAgentPools{
					readWithOptionsFn: func(_ context.Context, _ string, _ *tfe.AgentPoolReadOptions) (*tfe.AgentPool, error) {
						return &tfe.AgentPool{
							ID:         "apool-123",
							Workspaces: tt.workspaces,
						}, nil
					},
				},
				runs: &mockRuns{
					listFn: func(_ context.Context, wsID string, opts *tfe.RunListOptions) (*tfe.RunList, error) {
						statusCounts := tt.runsPerWS[wsID]
						count := statusCounts[opts.Status]
						items := make([]*tfe.Run, count)
						for i := range items {
							items[i] = &tfe.Run{ID: "run-placeholder"}
						}
						return &tfe.RunList{
							Items:      items,
							Pagination: &tfe.Pagination{TotalCount: count, TotalPages: 1, CurrentPage: 1},
						}, nil
					},
				},
			}

			count, err := c.GetPendingRuns(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("got %d, want %d", count, tt.wantCount)
			}
		})
	}
}
