package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "all required fields with defaults",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
				"ECS_CLUSTER":       "my-cluster",
				"ECS_SERVICE":       "tfc-agent",
			},
			want: Config{
				TFCToken:       "test-token",
				TFCAddress:     "https://app.terraform.io",
				TFCAgentPoolID: "apool-123",
				TFCOrg:         "my-org",
				ECSCluster:     "my-cluster",
				ECSService:     "tfc-agent",
				PollInterval:   10 * time.Second,
				MinAgents:      0,
				MaxAgents:      10,
				CooldownPeriod: 60 * time.Second,
				HealthAddr:     ":8080",
			},
		},
		{
			name: "all fields overridden",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFE_ADDRESS":       "https://tfe.example.com",
				"TFC_AGENT_POOL_ID": "apool-456",
				"TFC_ORG":           "other-org",
				"ECS_CLUSTER":       "prod-cluster",
				"ECS_SERVICE":       "tfc-agent-prod",
				"POLL_INTERVAL":     "30s",
				"MIN_AGENTS":        "2",
				"MAX_AGENTS":        "20",
				"COOLDOWN_PERIOD":   "120s",
				"HEALTH_ADDR":       ":9090",
			},
			want: Config{
				TFCToken:       "test-token",
				TFCAddress:     "https://tfe.example.com",
				TFCAgentPoolID: "apool-456",
				TFCOrg:         "other-org",
				ECSCluster:     "prod-cluster",
				ECSService:     "tfc-agent-prod",
				PollInterval:   30 * time.Second,
				MinAgents:      2,
				MaxAgents:      20,
				CooldownPeriod: 120 * time.Second,
				HealthAddr:     ":9090",
			},
		},
		{
			name:    "missing TFC_TOKEN",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name: "missing TFC_AGENT_POOL_ID",
			env: map[string]string{
				"TFC_TOKEN": "test-token",
			},
			wantErr: true,
		},
		{
			name: "missing TFC_ORG",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
			},
			wantErr: true,
		},
		{
			name: "missing ECS_CLUSTER",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
			},
			wantErr: true,
		},
		{
			name: "missing ECS_SERVICE",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
				"ECS_CLUSTER":       "my-cluster",
			},
			wantErr: true,
		},
		{
			name: "invalid POLL_INTERVAL",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
				"ECS_CLUSTER":       "my-cluster",
				"ECS_SERVICE":       "tfc-agent",
				"POLL_INTERVAL":     "not-a-duration",
			},
			wantErr: true,
		},
		{
			name: "invalid MIN_AGENTS",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
				"ECS_CLUSTER":       "my-cluster",
				"ECS_SERVICE":       "tfc-agent",
				"MIN_AGENTS":        "abc",
			},
			wantErr: true,
		},
		{
			name: "min greater than max",
			env: map[string]string{
				"TFC_TOKEN":         "test-token",
				"TFC_AGENT_POOL_ID": "apool-123",
				"TFC_ORG":           "my-org",
				"ECS_CLUSTER":       "my-cluster",
				"ECS_SERVICE":       "tfc-agent",
				"MIN_AGENTS":        "15",
				"MAX_AGENTS":        "5",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				v, ok := tt.env[key]
				return v, ok
			}

			got, err := load(lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
