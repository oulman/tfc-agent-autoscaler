// Package health provides HTTP health check and readiness probe endpoints.
package health

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// ReadinessProbe reports whether the application is ready to serve traffic.
type ReadinessProbe interface {
	IsReady() bool
}

// AtomicReady is a thread-safe readiness flag.
type AtomicReady struct {
	ready atomic.Bool
}

// IsReady returns true after MarkReady has been called.
func (a *AtomicReady) IsReady() bool {
	return a.ready.Load()
}

// MarkReady marks the application as ready. It is safe to call multiple times.
func (a *AtomicReady) MarkReady() {
	a.ready.Store(true)
}

// ChannelProbe adapts a channel to the ReadinessProbe interface.
// The probe reports ready once the channel is closed.
type ChannelProbe struct {
	ch <-chan struct{}
}

// NewChannelProbe creates a ChannelProbe from a readiness channel.
func NewChannelProbe(ch <-chan struct{}) *ChannelProbe {
	return &ChannelProbe{ch: ch}
}

// IsReady returns true after the channel has been closed.
func (p *ChannelProbe) IsReady() bool {
	select {
	case <-p.ch:
		return true
	default:
		return false
	}
}

// CompositeProbe aggregates multiple ReadinessProbes.
// It reports ready only when all sub-probes are ready.
type CompositeProbe struct {
	probes []ReadinessProbe
}

// NewCompositeProbe creates a CompositeProbe from the given probes.
func NewCompositeProbe(probes ...ReadinessProbe) *CompositeProbe {
	return &CompositeProbe{probes: probes}
}

// IsReady returns true only when all sub-probes are ready.
func (c *CompositeProbe) IsReady() bool {
	for _, p := range c.probes {
		if !p.IsReady() {
			return false
		}
	}
	return true
}

// ServerOption configures optional behavior for Server.
type ServerOption func(*Server)

// WithMetricsHandler registers an http.Handler for the /metrics endpoint.
func WithMetricsHandler(h http.Handler) ServerOption {
	return func(s *Server) {
		s.handler.Handle("GET /metrics", h)
	}
}

// Server serves health check endpoints.
type Server struct {
	httpServer *http.Server
	handler    *http.ServeMux
}

// NewServer creates a new health check server.
func NewServer(addr string, probe ReadinessProbe, opts ...ServerOption) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if probe.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready\n"))
	})

	s := &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      10 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		handler: mux,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Run starts the HTTP server and blocks until the context is canceled,
// then gracefully shuts down.
func (s *Server) Run(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	s.httpServer.Addr = ln.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck // intentional fresh context for graceful shutdown
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // shutdownCtx is derived from Background intentionally
			return err
		}
		// Drain the serve error (http.ErrServerClosed is expected).
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}
