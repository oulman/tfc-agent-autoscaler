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

## Dual-Service Mode (FARGATE_SPOT)

The autoscaler supports an optional dual-service mode that runs short-lived TFC jobs (plan, policy check, assessment) on FARGATE_SPOT while keeping long-running jobs (apply, stack_apply) on regular FARGATE. Plan-type jobs typically complete well within the 2-minute spot termination warning, making them safe candidates for spot pricing.

When enabled, the autoscaler creates two independent Scaler instances ("regular" and "spot"), each managing its own ECS service with its own min/max bounds, cooldown state, idle guard, and task protection. Both services register agents into the same TFC agent pool. A `ServiceView` layer filters agents and pending runs per-service using IP-based correlation against ECS task IPs.

Dual-service mode is opt-in via the `ECS_SPOT_SERVICE` environment variable. When not set, behavior is identical to single-service mode.

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

### Dual-Service Mode

| Variable | Required | Default | Description |
|---|---|---|---|
| `ECS_SPOT_SERVICE` | No | | Spot ECS service name (enables dual-service mode) |
| `SPOT_MIN_AGENTS` | No | `0` | Minimum agents for the spot service |
| `SPOT_MAX_AGENTS` | No | `10` | Maximum agents for the spot service |

## Endpoints

The health server (default `:8080`) exposes:

- `/healthz` — Liveness probe (always returns 200)
- `/readyz` — Readiness probe (returns 200 after the first successful reconciliation; in dual-service mode, requires both scalers to be ready)
- `/metrics` — Prometheus metrics

## Metrics

All metrics carry a `service` label (`"default"` in single-service mode, `"regular"` / `"spot"` in dual-service mode).

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

### With Dual-Service Mode

```sh
export TFC_TOKEN="your-token"
export TFC_AGENT_POOL_ID="apool-xxx"
export TFC_ORG="your-org"
export ECS_CLUSTER="your-cluster"
export ECS_SERVICE="tfc-agent"
export ECS_SPOT_SERVICE="tfc-agent-spot"
export SPOT_MIN_AGENTS=0
export SPOT_MAX_AGENTS=10

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

## Terraform Deployment

The `terraform/` directory contains a complete deployment configuration for the autoscaler and TFC agent services on ECS Fargate.

### ECR Pull-Through Cache

By default, the TFC agent image is pulled through an ECR pull-through cache rather than directly from Docker Hub. This avoids Docker Hub anonymous rate limits (100 pulls/6h) and reduces image pull latency since the image is cached in-region.

The resolved image URI follows the pattern:

```
<account_id>.dkr.ecr.<region>.amazonaws.com/docker-hub/hashicorp/tfc-agent:latest
```

To override and use a specific image instead (e.g. a private registry), set `tfc_agent_image`:

```hcl
tfc_agent_image = "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-tfc-agent:v1.0"
```

### Terraform Variables

In addition to the core deployment variables (`container_image`, `tfc_token`, `tfc_agent_pool_id`, etc.), the following control the TFC agent image:

| Variable | Default | Description |
|---|---|---|
| `tfc_agent_image` | `null` | Explicit image override; when null, uses the ECR pull-through cache |
| `tfc_agent_upstream_image` | `hashicorp/tfc-agent:latest` | Upstream Docker Hub image path |
| `ecr_cache_prefix` | `docker-hub` | ECR namespace prefix for cached images |

## Architecture

```
cmd/autoscaler/        Entry point
internal/
  config/              Environment variable configuration
  ecs/                 ECS client (service status, scaling, task protection)
  health/              Health check and metrics HTTP server (CompositeProbe for dual-service)
  metrics/             Prometheus metrics (service-labeled gauges/counters)
  scaler/              Autoscaling decision engine
  tfc/                 Terraform Cloud client (agents, pending runs, ServiceView filtering)
terraform/               ECS Fargate deployment (VPC, ECS cluster, agent services, ECR cache)
```
