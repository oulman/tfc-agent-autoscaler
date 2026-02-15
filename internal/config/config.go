package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ServiceConfig holds ECS service name and agent count bounds.
type ServiceConfig struct {
	ECSService string
	MinAgents  int
	MaxAgents  int
}

// Config holds all configuration for the autoscaler.
type Config struct {
	TFCToken       string
	TFCAddress     string
	TFCAgentPoolID string
	TFCOrg         string
	ECSCluster     string
	ECSService     string
	PollInterval   time.Duration
	MinAgents      int
	MaxAgents      int
	CooldownPeriod time.Duration
	HealthAddr     string
	SpotService    *ServiceConfig // nil = single-service mode
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

// load is the internal implementation that accepts a lookup function for testability.
func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		TFCAddress:     "https://app.terraform.io",
		PollInterval:   10 * time.Second,
		MinAgents:      0,
		MaxAgents:      10,
		CooldownPeriod: 60 * time.Second,
		HealthAddr:     ":8080",
	}

	required := []struct {
		dest *string
		key  string
	}{
		{&cfg.TFCToken, "TFC_TOKEN"},
		{&cfg.TFCAgentPoolID, "TFC_AGENT_POOL_ID"},
		{&cfg.TFCOrg, "TFC_ORG"},
		{&cfg.ECSCluster, "ECS_CLUSTER"},
		{&cfg.ECSService, "ECS_SERVICE"},
	}

	for _, r := range required {
		v, ok := lookup(r.key)
		if !ok || v == "" {
			return Config{}, fmt.Errorf("required environment variable %s is not set", r.key)
		}
		*r.dest = v
	}

	if v, ok := lookup("TFE_ADDRESS"); ok && v != "" {
		cfg.TFCAddress = v
	}

	if v, ok := lookup("POLL_INTERVAL"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid POLL_INTERVAL %q: %w", v, err)
		}
		cfg.PollInterval = d
	}

	if v, ok := lookup("COOLDOWN_PERIOD"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid COOLDOWN_PERIOD %q: %w", v, err)
		}
		cfg.CooldownPeriod = d
	}

	if v, ok := lookup("MIN_AGENTS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MIN_AGENTS %q: %w", v, err)
		}
		cfg.MinAgents = n
	}

	if v, ok := lookup("MAX_AGENTS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MAX_AGENTS %q: %w", v, err)
		}
		cfg.MaxAgents = n
	}

	if v, ok := lookup("HEALTH_ADDR"); ok && v != "" {
		cfg.HealthAddr = v
	}

	if cfg.MinAgents > cfg.MaxAgents {
		return Config{}, fmt.Errorf("MIN_AGENTS (%d) cannot be greater than MAX_AGENTS (%d)", cfg.MinAgents, cfg.MaxAgents)
	}

	if v, ok := lookup("ECS_SPOT_SERVICE"); ok && v != "" {
		spot := &ServiceConfig{
			ECSService: v,
			MinAgents:  0,
			MaxAgents:  10,
		}

		if v, ok := lookup("SPOT_MIN_AGENTS"); ok && v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("invalid SPOT_MIN_AGENTS %q: %w", v, err)
			}
			spot.MinAgents = n
		}

		if v, ok := lookup("SPOT_MAX_AGENTS"); ok && v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("invalid SPOT_MAX_AGENTS %q: %w", v, err)
			}
			spot.MaxAgents = n
		}

		if spot.MinAgents > spot.MaxAgents {
			return Config{}, fmt.Errorf("SPOT_MIN_AGENTS (%d) cannot be greater than SPOT_MAX_AGENTS (%d)", spot.MinAgents, spot.MaxAgents)
		}

		cfg.SpotService = spot
	}

	return cfg, nil
}
