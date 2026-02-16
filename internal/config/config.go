// Package config loads and validates autoscaler configuration from environment variables.
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

// lookupFn abstracts environment variable lookup for testability.
type lookupFn func(string) (string, bool)

func lookupDuration(lookup lookupFn, key string, dest *time.Duration) error {
	v, ok := lookup(key)
	if !ok || v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	*dest = d
	return nil
}

func lookupInt(lookup lookupFn, key string, dest *int) error {
	v, ok := lookup(key)
	if !ok || v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	*dest = n
	return nil
}

func lookupString(lookup lookupFn, key string, dest *string) {
	if v, ok := lookup(key); ok && v != "" {
		*dest = v
	}
}

// load is the internal implementation that accepts a lookup function for testability.
func load(lookup lookupFn) (Config, error) {
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

	lookupString(lookup, "TFE_ADDRESS", &cfg.TFCAddress)
	lookupString(lookup, "HEALTH_ADDR", &cfg.HealthAddr)

	if err := lookupDuration(lookup, "POLL_INTERVAL", &cfg.PollInterval); err != nil {
		return Config{}, err
	}
	if err := lookupDuration(lookup, "COOLDOWN_PERIOD", &cfg.CooldownPeriod); err != nil {
		return Config{}, err
	}
	if err := lookupInt(lookup, "MIN_AGENTS", &cfg.MinAgents); err != nil {
		return Config{}, err
	}
	if err := lookupInt(lookup, "MAX_AGENTS", &cfg.MaxAgents); err != nil {
		return Config{}, err
	}

	if cfg.MinAgents > cfg.MaxAgents {
		return Config{}, fmt.Errorf("MIN_AGENTS (%d) cannot be greater than MAX_AGENTS (%d)", cfg.MinAgents, cfg.MaxAgents)
	}

	if err := loadSpotConfig(lookup, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadSpotConfig(lookup lookupFn, cfg *Config) error {
	v, ok := lookup("ECS_SPOT_SERVICE")
	if !ok || v == "" {
		return nil
	}

	spot := &ServiceConfig{
		ECSService: v,
		MinAgents:  0,
		MaxAgents:  10,
	}

	if err := lookupInt(lookup, "SPOT_MIN_AGENTS", &spot.MinAgents); err != nil {
		return err
	}
	if err := lookupInt(lookup, "SPOT_MAX_AGENTS", &spot.MaxAgents); err != nil {
		return err
	}

	if spot.MinAgents > spot.MaxAgents {
		return fmt.Errorf("SPOT_MIN_AGENTS (%d) cannot be greater than SPOT_MAX_AGENTS (%d)", spot.MinAgents, spot.MaxAgents)
	}

	cfg.SpotService = spot
	return nil
}
