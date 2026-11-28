package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/adk/v2/telemetry"

	"github.com/rubics-code/agent-kit/kit/credentials"
)

// Run starts the agent and blocks until it stops.
//
// On a laptop it serves the console and A2A on Config.Addr, and runs a chat
// in this terminal. In container mode (<PREFIX>_HEADLESS=1) it serves the
// console and A2A on <PREFIX>_ADDR, answers /healthz on <PREFIX>_HEALTH_ADDR
// (all addresses by default), and stops cleanly on SIGTERM or SIGINT.
func Run(ctx context.Context, cfg Config) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer stop()
	var in io.Reader
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		in = os.Stdin
	}
	return Serve(ctx, cfg, credentials.FromOS(), in, os.Stdout, os.Stderr)
}

// Serve is Run with the environment and the terminal given. A nil in means
// no terminal chat: the agent serves until ctx ends.
func Serve(ctx context.Context, cfg Config, e credentials.Environ, in io.Reader, out, errOut io.Writer) error {
	flush, err := setupTelemetry(ctx, e, cfg.Name)
	if err != nil {
		return err
	}
	defer flush()
	a, err := Build(ctx, cfg, e, errOut)
	if err != nil {
		return err
	}
	defer a.Close()
	ln, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- srv.Serve(ln) }()

	var on []string
	for _, s := range a.Statuses {
		if s.Enabled {
			on = append(on, s.Name)
		}
	}
	groups := strings.Join(on, ", ")
	if groups == "" {
		groups = "none"
	}
	fmt.Fprintf(out, "%s (%s, keys: %s, groups: %s).\n", a.Name, a.Model, a.Mode, groups)

	var health *http.Server
	if a.Headless {
		addr := a.cfg.HealthAddr
		if v := a.cfg.env(e, "HEALTH_ADDR"); v != "" {
			addr = v
		}
		hl, err := net.Listen("tcp", addr)
		if err != nil {
			_ = srv.Close()
			return err
		}
		health = &http.Server{Handler: Health(), ReadHeaderTimeout: 5 * time.Second}
		go func() { errs <- health.Serve(hl) }()
		fmt.Fprintf(out, "Console and A2A on http://%s. Health: %s/healthz\n", ln.Addr(), hl.Addr())
	} else if in == nil {
		fmt.Fprintf(out, "Console: http://%s\n", ln.Addr())
	} else {
		fmt.Fprintf(out, "Console: http://%s\nType here to chat in the terminal, or exit to stop.\n\n", ln.Addr())
		go func() {
			// The end of stdin ends the chat, not the agent. Only exit stops it.
			if err := a.Chat(ctx, in, out); err != nil {
				errs <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shut)
	if health != nil {
		_ = health.Shutdown(shut)
	}
	if errors.Is(err, ErrExit) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Health answers GET /healthz.
func Health() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// setupTelemetry sends traces, metrics and logs over OTLP when
// OTEL_EXPORTER_OTLP_ENDPOINT (or a signal's own endpoint) is set. The token
// count of each model call goes with the metrics.
func setupTelemetry(ctx context.Context, e credentials.Environ, name string) (func(), error) {
	on := false
	for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"} {
		on = on || strings.TrimSpace(e[k]) != ""
	}
	if !on {
		return func() {}, nil
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", name)))
	if err != nil {
		return nil, err
	}
	p, err := telemetry.New(ctx, telemetry.WithResource(res))
	if err != nil {
		return nil, err
	}
	p.SetGlobalOtelProviders()
	return func() {
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// ADK's metric reader does not export on Shutdown, so the counts of
		// the last interval would be lost at a stop.
		if p.MeterProvider != nil {
			if err := p.MeterProvider.ForceFlush(shut); err != nil {
				slog.Warn("the metrics did not flush", "error", err)
			}
		}
		if err := p.Shutdown(shut); err != nil {
			slog.Warn("telemetry did not flush", "error", err)
		}
	}, nil
}
