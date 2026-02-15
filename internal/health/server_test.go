package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAtomicReady(t *testing.T) {
	ar := &AtomicReady{}
	if ar.IsReady() {
		t.Fatal("expected not ready initially")
	}

	ar.MarkReady()
	if !ar.IsReady() {
		t.Fatal("expected ready after MarkReady")
	}

	// Idempotent
	ar.MarkReady()
	if !ar.IsReady() {
		t.Fatal("expected still ready after second MarkReady")
	}
}

func TestHealthzHandler(t *testing.T) {
	srv := NewServer(":0", &AtomicReady{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok\n" {
		t.Errorf("got body %q, want %q", w.Body.String(), "ok\n")
	}
}

func TestReadyzHandlerNotReady(t *testing.T) {
	ar := &AtomicReady{}
	srv := NewServer(":0", ar)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if w.Body.String() != "not ready\n" {
		t.Errorf("got body %q, want %q", w.Body.String(), "not ready\n")
	}
}

func TestReadyzHandlerReady(t *testing.T) {
	ar := &AtomicReady{}
	ar.MarkReady()
	srv := NewServer(":0", ar)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok\n" {
		t.Errorf("got body %q, want %q", w.Body.String(), "ok\n")
	}
}

func TestChannelProbeNotReady(t *testing.T) {
	ch := make(chan struct{})
	probe := NewChannelProbe(ch)
	if probe.IsReady() {
		t.Fatal("expected not ready before channel is closed")
	}
}

func TestChannelProbeReady(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	probe := NewChannelProbe(ch)
	if !probe.IsReady() {
		t.Fatal("expected ready after channel is closed")
	}
}

func TestChannelProbeReadyIdempotent(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	probe := NewChannelProbe(ch)
	// Multiple calls should all return true.
	for range 3 {
		if !probe.IsReady() {
			t.Fatal("expected ready on repeated calls")
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# HELP test_metric A test metric\n"))
	})

	srv := NewServer(":0", &AtomicReady{}, WithMetricsHandler(metricsHandler))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "test_metric") {
		t.Errorf("body missing test_metric: %q", w.Body.String())
	}
}

func TestMetricsEndpointNotRegisteredWithoutOption(t *testing.T) {
	srv := NewServer(":0", &AtomicReady{})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d (no metrics handler configured)", w.Code, http.StatusNotFound)
	}
}

func TestCompositeProbeAllReady(t *testing.T) {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})
	close(ch1)
	close(ch2)

	probe := NewCompositeProbe(NewChannelProbe(ch1), NewChannelProbe(ch2))
	if !probe.IsReady() {
		t.Fatal("expected ready when all sub-probes are ready")
	}
}

func TestCompositeProbeOneNotReady(t *testing.T) {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})
	close(ch1)
	// ch2 not closed

	probe := NewCompositeProbe(NewChannelProbe(ch1), NewChannelProbe(ch2))
	if probe.IsReady() {
		t.Fatal("expected not ready when one sub-probe is not ready")
	}
}

func TestCompositeProbeNoneReady(t *testing.T) {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	probe := NewCompositeProbe(NewChannelProbe(ch1), NewChannelProbe(ch2))
	if probe.IsReady() {
		t.Fatal("expected not ready when no sub-probes are ready")
	}
}

func TestCompositeProbeEmpty(t *testing.T) {
	probe := NewCompositeProbe()
	if !probe.IsReady() {
		t.Fatal("expected ready when no sub-probes (vacuous truth)")
	}
}

func TestServerRunAndShutdown(t *testing.T) {
	srv := NewServer("127.0.0.1:0", &AtomicReady{})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Give server a moment to start
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}
