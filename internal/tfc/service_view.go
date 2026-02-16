package tfc

import (
	"context"
	"fmt"
)

// RunType identifies whether a ServiceView handles plan or apply runs.
type RunType int

// RunTypePlan and RunTypeApply distinguish plan-only vs apply-only service views.
const (
	RunTypePlan RunType = iota
	RunTypeApply
)

// ServiceViewClient is the subset of Client that ServiceView needs.
type ServiceViewClient interface {
	GetAgentDetails(ctx context.Context) ([]AgentInfo, error)
	GetPendingRunsByType(ctx context.Context) (PendingRunCounts, error)
}

// TaskIPsFunc returns the set of private IPs belonging to an ECS service's tasks.
type TaskIPsFunc func(ctx context.Context) (map[string]bool, error)

// ServiceView wraps a TFC Client to filter agents and runs for a specific ECS service.
// It implements the scaler.TFCClient interface.
type ServiceView struct {
	client  ServiceViewClient
	runType RunType
	taskIPs TaskIPsFunc
}

// NewServiceView creates a ServiceView that filters by run type and task IPs.
func NewServiceView(client ServiceViewClient, runType RunType, taskIPs TaskIPsFunc) *ServiceView {
	return &ServiceView{
		client:  client,
		runType: runType,
		taskIPs: taskIPs,
	}
}

// GetPendingRuns returns the pending run count for this service's run type.
func (sv *ServiceView) GetPendingRuns(ctx context.Context) (int, error) {
	counts, err := sv.client.GetPendingRunsByType(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting pending runs by type: %w", err)
	}

	switch sv.runType {
	case RunTypePlan:
		return counts.PlanPending, nil
	case RunTypeApply:
		return counts.ApplyPending, nil
	default:
		return 0, fmt.Errorf("unknown run type: %d", sv.runType)
	}
}

// GetAgentPoolStatus returns busy, idle, total counts for agents whose IPs
// match this service's ECS tasks.
func (sv *ServiceView) GetAgentPoolStatus(ctx context.Context) (busy, idle, total int, err error) {
	agents, err := sv.filteredAgents(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, agent := range agents {
		switch agent.Status {
		case "busy":
			busy++
			total++
		case "idle":
			idle++
			total++
		}
	}

	return busy, idle, total, nil
}

// GetAgentDetails returns agent details filtered to agents whose IPs
// match this service's ECS tasks.
func (sv *ServiceView) GetAgentDetails(ctx context.Context) ([]AgentInfo, error) {
	return sv.filteredAgents(ctx)
}

func (sv *ServiceView) filteredAgents(ctx context.Context) ([]AgentInfo, error) {
	allAgents, err := sv.client.GetAgentDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting agent details: %w", err)
	}

	ips, err := sv.taskIPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting task IPs: %w", err)
	}

	var filtered []AgentInfo
	for _, agent := range allAgents {
		if ips[agent.IP] {
			filtered = append(filtered, agent)
		}
	}

	return filtered, nil
}
