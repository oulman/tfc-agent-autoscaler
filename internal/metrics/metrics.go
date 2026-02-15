// Package metrics provides Prometheus metrics for the autoscaler.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus collectors for the autoscaler.
type Metrics struct {
	registry *prometheus.Registry

	pendingRuns     prometheus.Gauge
	busyAgents      prometheus.Gauge
	idleAgents      prometheus.Gauge
	totalAgents     prometheus.Gauge
	ecsDesiredCount prometheus.Gauge
	ecsRunningCount prometheus.Gauge

	reconcileTotal    *prometheus.CounterVec
	scaleEventsTotal  *prometheus.CounterVec
	cooldownSkipsTotal         prometheus.Counter
	taskProtectionErrorsTotal  prometheus.Counter
}

// New creates a new Metrics instance with a custom registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		pendingRuns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tfc_pending_runs",
			Help: "Number of queued TFC runs.",
		}),
		busyAgents: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tfc_busy_agents",
			Help: "Number of agents currently running jobs.",
		}),
		idleAgents: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tfc_idle_agents",
			Help: "Number of available agents.",
		}),
		totalAgents: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tfc_total_agents",
			Help: "Total number of agents in pool.",
		}),
		ecsDesiredCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ecs_desired_count",
			Help: "ECS desired task count.",
		}),
		ecsRunningCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ecs_running_count",
			Help: "ECS running task count.",
		}),
		reconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_reconcile_total",
			Help: "Total reconcile cycles.",
		}, []string{"result"}),
		scaleEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_scale_events_total",
			Help: "Scaling actions taken.",
		}, []string{"direction"}),
		cooldownSkipsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "autoscaler_cooldown_skips_total",
			Help: "Scale-downs blocked by cooldown.",
		}),
		taskProtectionErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "autoscaler_task_protection_errors_total",
			Help: "Total task protection API failures.",
		}),
	}

	reg.MustRegister(
		m.pendingRuns,
		m.busyAgents,
		m.idleAgents,
		m.totalAgents,
		m.ecsDesiredCount,
		m.ecsRunningCount,
		m.reconcileTotal,
		m.scaleEventsTotal,
		m.cooldownSkipsTotal,
		m.taskProtectionErrorsTotal,
	)

	return m
}

// Registry returns the custom prometheus registry.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Handler returns an http.Handler that serves the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RecordReconcile updates all gauge metrics with current values.
func (m *Metrics) RecordReconcile(busy, idle, total, pending, desired, running int) {
	m.pendingRuns.Set(float64(pending))
	m.busyAgents.Set(float64(busy))
	m.idleAgents.Set(float64(idle))
	m.totalAgents.Set(float64(total))
	m.ecsDesiredCount.Set(float64(desired))
	m.ecsRunningCount.Set(float64(running))
}

// RecordReconcileResult increments the reconcile counter with success or error.
func (m *Metrics) RecordReconcileResult(success bool) {
	if success {
		m.reconcileTotal.WithLabelValues("success").Inc()
	} else {
		m.reconcileTotal.WithLabelValues("error").Inc()
	}
}

// RecordScaleEvent increments the scale events counter.
func (m *Metrics) RecordScaleEvent(direction string) {
	m.scaleEventsTotal.WithLabelValues(direction).Inc()
}

// RecordCooldownSkip increments the cooldown skips counter.
func (m *Metrics) RecordCooldownSkip() {
	m.cooldownSkipsTotal.Inc()
}

// RecordTaskProtectionError increments the task protection error counter.
func (m *Metrics) RecordTaskProtectionError() {
	m.taskProtectionErrorsTotal.Inc()
}
