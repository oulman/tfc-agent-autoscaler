// Package tfc provides a client for querying Terraform Cloud/Enterprise
// agent pool status and pending runs.
package tfc

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-tfe"
)

// AgentPoolReader reads agent pool details including related workspaces.
type AgentPoolReader interface {
	ReadWithOptions(ctx context.Context, agentPoolID string, options *tfe.AgentPoolReadOptions) (*tfe.AgentPool, error)
}

// AgentLister lists agents within an agent pool.
type AgentLister interface {
	List(ctx context.Context, agentPoolID string, options *tfe.AgentListOptions) (*tfe.AgentList, error)
}

// RunLister lists runs for a workspace.
type RunLister interface {
	List(ctx context.Context, workspaceID string, options *tfe.RunListOptions) (*tfe.RunList, error)
}

// Client wraps TFC/TFE API access for the autoscaler.
type Client struct {
	agentPoolID string
	agentPools  AgentPoolReader
	agents      AgentLister
	runs        RunLister
}

// New creates a new TFC client.
func New(token, address, agentPoolID string) (*Client, error) {
	cfg := &tfe.Config{
		Token:   token,
		Address: address,
	}

	client, err := tfe.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating TFE client: %w", err)
	}

	return &Client{
		agentPoolID: agentPoolID,
		agentPools:  client.AgentPools,
		agents:      client.Agents,
		runs:        client.Runs,
	}, nil
}

// AgentInfo holds details about a single TFC agent.
type AgentInfo struct {
	ID     string
	Name   string
	IP     string
	Status string
}

// GetAgentDetails returns detailed information about all agents in the pool.
func (c *Client) GetAgentDetails(ctx context.Context) ([]AgentInfo, error) {
	opts := &tfe.AgentListOptions{
		ListOptions: tfe.ListOptions{PageSize: 100},
	}

	var agents []AgentInfo
	for {
		list, err := c.agents.List(ctx, c.agentPoolID, opts)
		if err != nil {
			return nil, fmt.Errorf("listing agents: %w", err)
		}

		for _, agent := range list.Items {
			agents = append(agents, AgentInfo{
				ID:     agent.ID,
				Name:   agent.Name,
				IP:     agent.IP,
				Status: agent.Status,
			})
		}

		if list.Pagination == nil || list.CurrentPage >= list.TotalPages {
			break
		}
		opts.PageNumber = list.NextPage
	}

	return agents, nil
}

// GetAgentPoolStatus returns the count of busy, idle, and total agents in the pool.
func (c *Client) GetAgentPoolStatus(ctx context.Context) (busy, idle, total int, err error) {
	opts := &tfe.AgentListOptions{
		ListOptions: tfe.ListOptions{PageSize: 100},
	}

	for {
		agents, listErr := c.agents.List(ctx, c.agentPoolID, opts)
		if listErr != nil {
			return 0, 0, 0, fmt.Errorf("listing agents: %w", listErr)
		}

		for _, agent := range agents.Items {
			total++
			switch agent.Status {
			case "busy":
				busy++
			case "idle":
				idle++
			}
		}

		if agents.Pagination == nil || agents.CurrentPage >= agents.TotalPages {
			break
		}
		opts.PageNumber = agents.NextPage
	}

	return busy, idle, total, nil
}

// planPendingStatuses filters runs waiting for plan capacity.
var planPendingStatuses = strings.Join([]string{
	string(tfe.RunPending),
	string(tfe.RunPlanQueued),
}, ",")

// applyPendingStatuses filters runs waiting for apply capacity.
var applyPendingStatuses = strings.Join([]string{
	string(tfe.RunApplyQueued),
}, ",")

// PendingRunCounts holds pending run counts split by type.
type PendingRunCounts struct {
	PlanPending  int
	ApplyPending int
}

// Total returns the sum of plan and apply pending runs.
func (p PendingRunCounts) Total() int {
	return p.PlanPending + p.ApplyPending
}

// GetPendingRunsByType returns pending run counts split by plan vs apply type
// across all workspaces assigned to this agent pool.
func (c *Client) GetPendingRunsByType(ctx context.Context) (PendingRunCounts, error) {
	pool, err := c.agentPools.ReadWithOptions(ctx, c.agentPoolID, &tfe.AgentPoolReadOptions{
		Include: []tfe.AgentPoolIncludeOpt{tfe.AgentPoolWorkspaces},
	})
	if err != nil {
		return PendingRunCounts{}, fmt.Errorf("reading agent pool: %w", err)
	}

	var counts PendingRunCounts
	for _, ws := range pool.Workspaces {
		planCount, err := c.countRunsForWorkspace(ctx, ws.ID, planPendingStatuses)
		if err != nil {
			return PendingRunCounts{}, fmt.Errorf("counting plan runs for workspace %s: %w", ws.ID, err)
		}
		counts.PlanPending += planCount

		applyCount, err := c.countRunsForWorkspace(ctx, ws.ID, applyPendingStatuses)
		if err != nil {
			return PendingRunCounts{}, fmt.Errorf("counting apply runs for workspace %s: %w", ws.ID, err)
		}
		counts.ApplyPending += applyCount
	}

	return counts, nil
}

// GetPendingRuns returns the total count of pending/queued runs across all
// workspaces assigned to this agent pool.
func (c *Client) GetPendingRuns(ctx context.Context) (int, error) {
	counts, err := c.GetPendingRunsByType(ctx)
	if err != nil {
		return 0, err
	}
	return counts.Total(), nil
}

func (c *Client) countRunsForWorkspace(ctx context.Context, workspaceID, statuses string) (int, error) {
	runs, err := c.runs.List(ctx, workspaceID, &tfe.RunListOptions{
		Status: statuses,
	})
	if err != nil {
		return 0, err
	}

	if runs.Pagination != nil {
		return runs.TotalCount, nil
	}
	return len(runs.Items), nil
}
