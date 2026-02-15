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

	assertGaugeValue(t, m.pendingRuns, 4)
	assertGaugeValue(t, m.busyAgents, 3)
	assertGaugeValue(t, m.idleAgents, 2)
	assertGaugeValue(t, m.totalAgents, 5)
	assertGaugeValue(t, m.ecsDesiredCount, 6)
	assertGaugeValue(t, m.ecsRunningCount, 5)
}

func TestRecordReconcileSuccess(t *testing.T) {
	m := New()
	m.RecordReconcileResult(true)
	m.RecordReconcileResult(true)
	m.RecordReconcileResult(false)

	assertCounterValue(t, m.reconcileTotal, "result", "success", 2)
	assertCounterValue(t, m.reconcileTotal, "result", "error", 1)
}

func TestRecordScaleEvent(t *testing.T) {
	m := New()
	m.RecordScaleEvent("up")
	m.RecordScaleEvent("up")
	m.RecordScaleEvent("down")

	assertCounterValue(t, m.scaleEventsTotal, "direction", "up", 2)
	assertCounterValue(t, m.scaleEventsTotal, "direction", "down", 1)
}

func TestRecordCooldownSkip(t *testing.T) {
	m := New()
	m.RecordCooldownSkip()
	m.RecordCooldownSkip()

	assertPlainCounterValue(t, m.cooldownSkipsTotal, 2)
}

func TestRecordTaskProtectionError(t *testing.T) {
	m := New()
	m.RecordTaskProtectionError()
	m.RecordTaskProtectionError()

	assertPlainCounterValue(t, m.taskProtectionErrorsTotal, 2)
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

func assertGaugeValue(t *testing.T, g prometheus.Gauge, want float64) {
	t.Helper()
	m := &io_prometheus_client.Metric{}
	if err := g.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetGauge().GetValue()
	if got != want {
		t.Errorf("gauge value = %v, want %v", got, want)
	}
}

func assertCounterValue(t *testing.T, cv *prometheus.CounterVec, labelName, labelValue string, want float64) {
	t.Helper()
	c, err := cv.GetMetricWithLabelValues(labelValue)
	if err != nil {
		t.Fatalf("getting counter with label %s=%s: %v", labelName, labelValue, err)
	}
	m := &io_prometheus_client.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got != want {
		t.Errorf("counter(%s=%s) = %v, want %v", labelName, labelValue, got, want)
	}
}

func assertPlainCounterValue(t *testing.T, c prometheus.Counter, want float64) {
	t.Helper()
	m := &io_prometheus_client.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	got := m.GetCounter().GetValue()
	if got != want {
		t.Errorf("counter = %v, want %v", got, want)
	}
}
