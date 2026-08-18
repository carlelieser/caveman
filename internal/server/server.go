package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/telemetry"
)

const DefaultPort = 8787

// RegisteredProviders is what Caveman serves. Adding a provider is adding an
// entry here; no handler learns its name.
func RegisteredProviders() adapters.Registry {
	return adapters.Registry{anthropic.New()}
}

// Options configures one server. Every field has a working default, so a bare
// New() is a transparent proxy for the registered providers.
type Options struct {
	Registry adapters.Registry
	Stage    CompressionStage
	Reporter *telemetry.Reporter
	Client   *http.Client
}

// Server is a router paired with the reporter tallying its session.
type Server struct {
	Handler  http.Handler
	Reporter *telemetry.Reporter
	Registry adapters.Registry
}

func New(options Options) *Server {
	registry := options.Registry
	if registry == nil {
		registry = RegisteredProviders()
	}
	stage := options.Stage
	if stage == nil {
		stage = IdentityStage
	}
	client := options.Client
	if client == nil {
		// No client-side timeout: a long generation is a slow response, not a
		// stalled one, and the client's own cancellation already bounds it.
		client = &http.Client{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, handleHealth)
	for _, provider := range registry {
		handler := route{provider: provider, stage: stage, reporter: options.Reporter, client: client}
		// The route comes from the provider, never from a constant here. A path
		// no provider claims stays unregistered and answers 404 without ever
		// reaching upstream.
		mux.HandleFunc("POST "+provider.Path(), handler.handle)
	}

	return &Server{Handler: mux, Reporter: options.Reporter, Registry: registry}
}

// ListenPort reads PORT, falling back to the default. An unusable value is an
// error rather than a silent fallback, so a typo is not mistaken for a running
// proxy on the wrong port.
func ListenPort() (int, error) {
	configured, present := os.LookupEnv("PORT")
	if !present || strings.TrimSpace(configured) == "" {
		return DefaultPort, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(configured))
	if err != nil || parsed < 0 || parsed > 65535 {
		return 0, &InvalidPortError{Value: configured}
	}
	return parsed, nil
}

type InvalidPortError struct{ Value string }

func (e *InvalidPortError) Error() string {
	return "reading PORT failed: " + strconv.Quote(e.Value) + " is not a valid port"
}
