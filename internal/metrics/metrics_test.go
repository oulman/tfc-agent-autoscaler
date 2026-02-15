package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}
	if m.Registry() == nil {
		t.Fatal("expected non-nil Registry")
	}
}

func TestRecordReconcile(t *testing.T) {
	m := New()
	m.RecordReconcile(3, 2, 5, 4, 6, 5)

	assertGaugeVecValue(t, m.pendingRuns, "default", 4)
	assertGaugeVecValue(t, m.busyAgents, "default", 3)
	assertGaugeVecValue(t, m.idleAgents, "default", 2)
	assertGaugeVecValue(t, m.totalAgents, "default", 5)
	assertGaugeVecValue(t, m.ecsDesiredCount, "default", 6)
	assertGaugeVecValue(t, m.ecsRunningCount, "default", 5)
}

func TestRecordReconcileSuccess(t *testing.T) {
	m := New()
	m.RecordReconcileResult(true)
	m.RecordReconcileResult(true)
	m.RecordReconcileResult(false)

	assertCounterVecValue(t, m.reconcileTotal, "default", "success", 2)
	assertCounterVecValue(t, m.reconcileTotal, "default", "error", 1)
}

func TestRecordScaleEvent(t *testing.T) {
	m := New()
	m.RecordScaleEvent("up")
	m.RecordScaleEvent("up")
	m.RecordScaleEvent("down")

	assertCounterVecValue(t, m.scaleEventsTotal, "default", "up", 2)
	assertCounterVecValue(t, m.scaleEventsTotal, "default", "down", 1)
}

func TestRecordCooldownSkip(t *testing.T) {
	m := New()
	m.RecordCooldownSkip()
	m.RecordCooldownSkip()

	assertCounterVecSingleLabel(t, m.cooldownSkipsTotal, "default", 2)
}

func TestRecordTaskProtectionError(t *testing.T) {
	m := New()
	m.RecordTaskProtectionError()
	m.RecordTaskProtectionError()

	assertCounterVecSingleLabel(t, m.taskProtectionErrorsTotal, "default", 2)
}

func TestHTTPHandler(t *testing.T) {
	m := New()
	m.RecordReconcile(1, 0, 1, 2, 3, 3)
	m.RecordReconcileResult(true)
	m.RecordScaleEvent("up")
	m.RecordCooldownSkip()

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	for _, want := range []string{
		"tfc_pending_runs",
		"tfc_busy_agents",
		"tfc_idle_agents",
		"tfc_total_agents",
		"ecs_desired_count",
		"ecs_running_count",
		"autoscaler_reconcile_total",
		"autoscaler_scale_events_total",
		"autoscaler_cooldown_skips_total",
		"autoscaler_task_protection_errors_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestForService(t *testing.T) {
	m := New()
	sm := m.ForService("spot")

	sm.RecordReconcile(3, 2, 5, 4, 6, 5)
	sm.RecordReconcileResult(true)
	sm.RecordScaleEvent("up")
	sm.RecordCooldownSkip()
	sm.RecordTaskProtectionError()

	assertGaugeVecValue(t, m.pendingRuns, "spot", 4)
	assertGaugeVecValue(t, m.busyAgents, "spot", 3)
	assertGaugeVecValue(t, m.idleAgents, "spot", 2)
	assertGaugeVecValue(t, m.totalAgents, "spot", 5)
	assertGaugeVecValue(t, m.ecsDesiredCount, "spot", 6)
	assertGaugeVecValue(t, m.ecsRunningCount, "spot", 5)

	assertCounterVecValue(t, m.reconcileTotal, "spot", "success", 1)
	assertCounterVecValue(t, m.scaleEventsTotal, "spot", "up", 1)
	assertCounterVecSingleLabel(t, m.cooldownSkipsTotal, "spot", 1)
	assertCounterVecSingleLabel(t, m.taskProtectionErrorsTotal, "spot", 1)
}

func TestForServiceIsolation(t *testing.T) {
	m := New()
	regular := m.ForService("regular")
	spot := m.ForService("spot")

	regular.RecordReconcile(1, 0, 1, 2, 3, 3)
	spot.RecordReconcile(4, 1, 5, 6, 7, 7)

	assertGaugeVecValue(t, m.busyAgents, "regular", 1)
	assertGaugeVecValue(t, m.busyAgents, "spot", 4)
}

func assertGaugeVecValue(t *testing.T, gv *prometheus.GaugeVec, service string, want float64) {
	t.Helper()
	g, err := gv.GetMetricWithLabelValues(service)
	if err != nil {
		t.Fatalf("getting gauge with service=%s: %v", service, err)
	}
	m := &io_prometheus_client.Metric{}
	if err := g.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetGauge().GetValue()
	if got != want {
		t.Errorf("gauge(service=%s) = %v, want %v", service, got, want)
	}
}

// assertCounterVecValue asserts a counter in a 2-label CounterVec (service + another label).
func assertCounterVecValue(t *testing.T, cv *prometheus.CounterVec, service, secondLabel string, want float64) {
	t.Helper()
	c, err := cv.GetMetricWithLabelValues(service, secondLabel)
	if err != nil {
		t.Fatalf("getting counter with labels %s, %s: %v", service, secondLabel, err)
	}
	m := &io_prometheus_client.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got != want {
		t.Errorf("counter(%s, %s) = %v, want %v", service, secondLabel, got, want)
	}
}

// assertCounterVecSingleLabel asserts a counter in a single-label CounterVec (service only).
func assertCounterVecSingleLabel(t *testing.T, cv *prometheus.CounterVec, service string, want float64) {
	t.Helper()
	c, err := cv.GetMetricWithLabelValues(service)
	if err != nil {
		t.Fatalf("getting counter with service=%s: %v", service, err)
	}
	m := &io_prometheus_client.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got != want {
		t.Errorf("counter(service=%s) = %v, want %v", service, got, want)
	}
}
