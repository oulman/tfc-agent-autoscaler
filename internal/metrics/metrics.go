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

	pendingRuns     *prometheus.GaugeVec
	busyAgents      *prometheus.GaugeVec
	idleAgents      *prometheus.GaugeVec
	totalAgents     *prometheus.GaugeVec
	ecsDesiredCount *prometheus.GaugeVec
	ecsRunningCount *prometheus.GaugeVec

	reconcileTotal             *prometheus.CounterVec
	scaleEventsTotal           *prometheus.CounterVec
	cooldownSkipsTotal         *prometheus.CounterVec
	taskProtectionErrorsTotal  *prometheus.CounterVec
}

// New creates a new Metrics instance with a custom registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		pendingRuns: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tfc_pending_runs",
			Help: "Number of queued TFC runs.",
		}, []string{"service"}),
		busyAgents: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tfc_busy_agents",
			Help: "Number of agents currently running jobs.",
		}, []string{"service"}),
		idleAgents: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tfc_idle_agents",
			Help: "Number of available agents.",
		}, []string{"service"}),
		totalAgents: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tfc_total_agents",
			Help: "Total number of agents in pool.",
		}, []string{"service"}),
		ecsDesiredCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ecs_desired_count",
			Help: "ECS desired task count.",
		}, []string{"service"}),
		ecsRunningCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ecs_running_count",
			Help: "ECS running task count.",
		}, []string{"service"}),
		reconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_reconcile_total",
			Help: "Total reconcile cycles.",
		}, []string{"service", "result"}),
		scaleEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_scale_events_total",
			Help: "Scaling actions taken.",
		}, []string{"service", "direction"}),
		cooldownSkipsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_cooldown_skips_total",
			Help: "Scale-downs blocked by cooldown.",
		}, []string{"service"}),
		taskProtectionErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_task_protection_errors_total",
			Help: "Total task protection API failures.",
		}, []string{"service"}),
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

// ForService returns a ServiceMetrics that records metrics with the given service label.
func (m *Metrics) ForService(name string) *ServiceMetrics {
	return &ServiceMetrics{
		pendingRuns:     m.pendingRuns.WithLabelValues(name),
		busyAgents:      m.busyAgents.WithLabelValues(name),
		idleAgents:      m.idleAgents.WithLabelValues(name),
		totalAgents:     m.totalAgents.WithLabelValues(name),
		ecsDesiredCount: m.ecsDesiredCount.WithLabelValues(name),
		ecsRunningCount: m.ecsRunningCount.WithLabelValues(name),
		reconcileSuccess: m.reconcileTotal.WithLabelValues(name, "success"),
		reconcileError:   m.reconcileTotal.WithLabelValues(name, "error"),
		scaleUp:          m.scaleEventsTotal.WithLabelValues(name, "up"),
		scaleDown:        m.scaleEventsTotal.WithLabelValues(name, "down"),
		cooldownSkips:    m.cooldownSkipsTotal.WithLabelValues(name),
		taskProtErrors:   m.taskProtectionErrorsTotal.WithLabelValues(name),
	}
}

// RecordReconcile updates all gauge metrics with current values (default service).
func (m *Metrics) RecordReconcile(busy, idle, total, pending, desired, running int) {
	m.ForService("default").RecordReconcile(busy, idle, total, pending, desired, running)
}

// RecordReconcileResult increments the reconcile counter with success or error (default service).
func (m *Metrics) RecordReconcileResult(success bool) {
	m.ForService("default").RecordReconcileResult(success)
}

// RecordScaleEvent increments the scale events counter (default service).
func (m *Metrics) RecordScaleEvent(direction string) {
	m.ForService("default").RecordScaleEvent(direction)
}

// RecordCooldownSkip increments the cooldown skips counter (default service).
func (m *Metrics) RecordCooldownSkip() {
	m.ForService("default").RecordCooldownSkip()
}

// RecordTaskProtectionError increments the task protection error counter (default service).
func (m *Metrics) RecordTaskProtectionError() {
	m.ForService("default").RecordTaskProtectionError()
}

// ServiceMetrics records metrics for a specific service.
type ServiceMetrics struct {
	pendingRuns      prometheus.Gauge
	busyAgents       prometheus.Gauge
	idleAgents       prometheus.Gauge
	totalAgents      prometheus.Gauge
	ecsDesiredCount  prometheus.Gauge
	ecsRunningCount  prometheus.Gauge
	reconcileSuccess prometheus.Counter
	reconcileError   prometheus.Counter
	scaleUp          prometheus.Counter
	scaleDown        prometheus.Counter
	cooldownSkips    prometheus.Counter
	taskProtErrors   prometheus.Counter
}

// RecordReconcile updates all gauge metrics with current values.
func (sm *ServiceMetrics) RecordReconcile(busy, idle, total, pending, desired, running int) {
	sm.pendingRuns.Set(float64(pending))
	sm.busyAgents.Set(float64(busy))
	sm.idleAgents.Set(float64(idle))
	sm.totalAgents.Set(float64(total))
	sm.ecsDesiredCount.Set(float64(desired))
	sm.ecsRunningCount.Set(float64(running))
}

// RecordReconcileResult increments the reconcile counter with success or error.
func (sm *ServiceMetrics) RecordReconcileResult(success bool) {
	if success {
		sm.reconcileSuccess.Inc()
	} else {
		sm.reconcileError.Inc()
	}
}

// RecordScaleEvent increments the scale events counter.
func (sm *ServiceMetrics) RecordScaleEvent(direction string) {
	switch direction {
	case "up":
		sm.scaleUp.Inc()
	case "down":
		sm.scaleDown.Inc()
	}
}

// RecordCooldownSkip increments the cooldown skips counter.
func (sm *ServiceMetrics) RecordCooldownSkip() {
	sm.cooldownSkips.Inc()
}

// RecordTaskProtectionError increments the task protection error counter.
func (sm *ServiceMetrics) RecordTaskProtectionError() {
	sm.taskProtErrors.Inc()
}
