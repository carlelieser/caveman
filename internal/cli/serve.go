package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/carlelieser/caveman/internal/compress"
	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/server"
	"github.com/carlelieser/caveman/internal/telemetry"
)

// ServeRequested reports whether this process was spawned as the daemon rather
// than invoked as the CLI.
func ServeRequested(lookup func(string) (string, bool)) bool {
	value, ok := lookup(serveEnvVar)
	return ok && value != "" && value != "0"
}

// enabledScopes are the scopes the policy left on, in the walk's own order.
func enabledScopes(p policy.Policy) []ir.Scope {
	scopes := make([]ir.Scope, 0, len(ir.AllScopes))
	for _, scope := range ir.AllScopes {
		if p.Scope[policy.ScopeName(scope)] {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// CompressionStage is the seam between the handler and the pipeline. A request
// that asked for nothing is forwarded without a walk.
func CompressionStage(request ir.Request, p policy.Policy) server.StageResult {
	if !p.CompressionEnabled() {
		return server.StageResult{Request: request}
	}
	result := compress.RunPipeline(compress.PipelineRequest{
		Request:   request,
		Level:     p.Level,
		Scopes:    enabledScopes(p),
		CacheMode: p.CacheMode,
		Count:     p.Count,
	})
	stats := result.Stats
	return server.StageResult{Request: result.Request, Stats: &stats}
}

const shutdownGrace = 5 * time.Second

// Serve runs the proxy until a shutdown signal arrives, writing the session
// total on the way out. A hard kill skips it, which is why the CLI's stop path
// sends SIGTERM first.
func Serve(streams Streams) int {
	port, err := server.ListenPort()
	if err != nil {
		streams.warn("caveman: %s", err)
		return ExitFailure
	}

	sink := telemetry.NewSinkTo(streams.Out)
	reporter := telemetry.NewReporter(sink)
	proxy := server.New(server.Options{Stage: CompressionStage, Reporter: reporter})

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		streams.warn("caveman: binding port %d failed: %s", port, err)
		return ExitFailure
	}

	httpServer := &http.Server{Handler: proxy.Handler}
	errs := make(chan error, 1)
	go func() { errs <- httpServer.Serve(listener) }()

	streams.say("caveman listening on http://localhost:%d", port)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			streams.warn("caveman: %s", err)
			return ExitFailure
		}
	case <-signals:
		shutdown, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}

	if summary, ok := reporter.Summary(); ok {
		sink(summary)
	}
	return ExitOK
}
