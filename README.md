# tfc-agent-autoscaler

Autoscaler for [Terraform Cloud/Enterprise](https://www.terraform.io/) agent pools running on AWS ECS Fargate. It monitors pending runs and agent status in TFC, then adjusts the ECS service desired count to match demand.

## How It Works

The autoscaler runs a reconciliation loop on a configurable interval:

1. Queries TFC for busy/idle agents and pending runs across all workspaces assigned to the agent pool.
2. Computes a desired agent count: `desired = clamp(pendingRuns + busyAgents, min, max)`.
3. Compares against the current ECS service desired count and scales up or down as needed.

**Scale-up** is immediate. **Scale-down** respects a configurable cooldown period and includes two layers of protection to avoid killing agents mid-run:

- **Idle Guard** — Scale-down is capped so the service never reduces below the number of busy agents. Only idle agents are removed.
- **ECS Task Scale-In Protection** — Busy agents' ECS tasks are marked with scale-in protection so ECS can only terminate idle ones. If the protection API fails, the idle guard alone still prevents unsafe termination.

Agent-to-task correlation uses IP matching: TFC agents expose their IP, and Fargate tasks each get a private IP via their ENI. The autoscaler matches these to determine which tasks are busy or idle.

## Configuration

All configuration is via environment variables.

| Variable | Required | Default | Description |
|---|---|---|---|
| `TFC_TOKEN` | Yes | | Terraform Cloud API token |
| `TFC_AGENT_POOL_ID` | Yes | | Agent pool ID to monitor |
| `TFC_ORG` | Yes | | Terraform Cloud organization |
| `ECS_CLUSTER` | Yes | | ECS cluster name |
| `ECS_SERVICE` | Yes | | ECS service name |
| `TFE_ADDRESS` | No | `https://app.terraform.io` | TFC/TFE API address |
| `POLL_INTERVAL` | No | `10s` | How often to reconcile |
| `COOLDOWN_PERIOD` | No | `60s` | Minimum time between scale-down events |
| `MIN_AGENTS` | No | `0` | Minimum number of agents to maintain |
| `MAX_AGENTS` | No | `10` | Maximum number of agents allowed |
| `HEALTH_ADDR` | No | `:8080` | Address for health/metrics server |

## Endpoints

The health server (default `:8080`) exposes:

- `/healthz` — Liveness probe (always returns 200)
- `/readyz` — Readiness probe (returns 200 after the first successful reconciliation)
- `/metrics` — Prometheus metrics

## Metrics

| Metric | Type | Description |
|---|---|---|
| `tfc_pending_runs` | Gauge | Queued TFC runs |
| `tfc_busy_agents` | Gauge | Agents currently running jobs |
| `tfc_idle_agents` | Gauge | Available agents |
| `tfc_total_agents` | Gauge | Total agents in pool |
| `ecs_desired_count` | Gauge | ECS desired task count |
| `ecs_running_count` | Gauge | ECS running task count |
| `autoscaler_reconcile_total` | Counter | Reconcile cycles (labeled `result=success\|error`) |
| `autoscaler_scale_events_total` | Counter | Scaling actions (labeled `direction=up\|down`) |
| `autoscaler_cooldown_skips_total` | Counter | Scale-downs blocked by cooldown |
| `autoscaler_task_protection_errors_total` | Counter | Task protection API failures |

## Building

```sh
# Build binary
make build

# Run tests
make test

# Build Docker image
make docker

# Build with custom tag
make docker TAG=v1.0.0
```

## Running

### Locally

```sh
export TFC_TOKEN="your-token"
export TFC_AGENT_POOL_ID="apool-xxx"
export TFC_ORG="your-org"
export ECS_CLUSTER="your-cluster"
export ECS_SERVICE="tfc-agent"

./autoscaler
```

### Docker

```sh
docker run \
  -e TFC_TOKEN="your-token" \
  -e TFC_AGENT_POOL_ID="apool-xxx" \
  -e TFC_ORG="your-org" \
  -e ECS_CLUSTER="your-cluster" \
  -e ECS_SERVICE="tfc-agent" \
  -p 8080:8080 \
  tfc-agent-autoscaler:latest
```

### ECS

Deploy as its own ECS service alongside the agent service. The autoscaler needs:

- **IAM permissions**: `ecs:DescribeServices`, `ecs:UpdateService`, `ecs:ListTasks`, `ecs:DescribeTasks`, `ecs:UpdateTaskProtection` on the agent service.
- **Network access**: The task must be able to reach the TFC/TFE API and the ECS API.

## Architecture

```
cmd/autoscaler/        Entry point
internal/
  config/              Environment variable configuration
  ecs/                 ECS client (service status, scaling, task protection)
  health/              Health check and metrics HTTP server
  metrics/             Prometheus metrics
  scaler/              Autoscaling decision engine
  tfc/                 Terraform Cloud client (agents, pending runs)
```
