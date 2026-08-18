package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/telemetry"
)

const globalOverrideVariable = "CAVEMAN_UPSTREAM_BASE_URL"

// Headers that describe the client's connection to Caveman rather than the
// request itself. Forwarding them breaks the upstream call: a stale
// content-length disagrees with the re-serialized body, host points at Caveman,
// and hop-by-hop headers belong to the finished connection.
var connectionHeaderNames = []string{
	"host",
	"content-length",
	"connection",
	"keep-alive",
	"proxy-authorization",
	"proxy-connection",
	"te",
	"trailer",
	"transfer-encoding",
	"upgrade",
	"expect",
}

// Decoding is left to the transport so the SSE stream arrives as plain text
// chunks rather than a compressed frame that would batch tokens.
var negotiationHeaderNames = []string{"accept-encoding"}

var strippedRequestHeaderNames = buildStripSet(
	connectionHeaderNames,
	negotiationHeaderNames,
	policy.CavemanHeaderNames,
)

// Response headers describing upstream's transfer encoding, which no longer
// apply once the body has been decoded and re-framed to the client.
var strippedResponseHeaderNames = buildStripSet([]string{
	"content-encoding",
	"content-length",
	"connection",
	"keep-alive",
	"transfer-encoding",
})

func buildStripSet(groups ...[]string) map[string]bool {
	set := map[string]bool{}
	for _, group := range groups {
		for _, name := range group {
			set[strings.ToLower(name)] = true
		}
	}
	return set
}

var nonAlphanumeric = regexp.MustCompile(`[^A-Z0-9]+`)

// overrideVariableName is the per-provider override, e.g. CAVEMAN_ANTHROPIC_BASE_URL.
func overrideVariableName(providerName string) string {
	normalized := nonAlphanumeric.ReplaceAllString(strings.ToUpper(providerName), "_")
	return "CAVEMAN_" + normalized + "_BASE_URL"
}

func readOverride(name string) (string, bool) {
	configured, present := os.LookupEnv(name)
	if !present || strings.TrimSpace(configured) == "" {
		return "", false
	}
	return strings.TrimRight(strings.TrimSpace(configured), "/"), true
}

// UpstreamBaseURL is the provider's own base URL, unless an env override
// redirects it. The provider-wide override applies to every provider and exists
// so a test or a local run can point all traffic at one host.
func UpstreamBaseURL(provider adapters.Provider) string {
	if specific, ok := readOverride(overrideVariableName(provider.Name())); ok {
		return specific
	}
	if global, ok := readOverride(globalOverrideVariable); ok {
		return global
	}
	return provider.BaseURL()
}

// forwardableRequestHeaders copies the client's headers verbatim except those
// that would misdescribe the new request. Credentials pass through untouched
// and are never read.
func forwardableRequestHeaders(incoming http.Header) http.Header {
	outgoing := http.Header{}
	for name, values := range incoming {
		if strippedRequestHeaderNames[strings.ToLower(name)] {
			continue
		}
		for _, value := range values {
			outgoing.Add(name, value)
		}
	}
	return outgoing
}

func forwardableResponseHeaders(incoming http.Header) http.Header {
	outgoing := http.Header{}
	for name, values := range incoming {
		if strippedResponseHeaderNames[strings.ToLower(name)] {
			continue
		}
		for _, value := range values {
			outgoing.Add(name, value)
		}
	}
	return outgoing
}

type upstreamRequest struct {
	provider adapters.Provider
	headers  http.Header
	body     string
	// search is the incoming query string, including its leading `?`.
	search string
}

// sendUpstream forwards one request. The context carries the client's
// cancellation, so a client that hangs up aborts the upstream call rather than
// leaving it running to completion against a connection nobody is reading.
func sendUpstream(ctx context.Context, client *http.Client, request upstreamRequest) (*http.Response, error) {
	url := UpstreamBaseURL(request.provider) + request.provider.Path() + request.search
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(request.body))
	if err != nil {
		return nil, fmt.Errorf("building upstream request to %s: %w", url, err)
	}
	outgoing.Header = request.headers
	// Set explicitly so the transport frames a fixed-length body rather than
	// falling back to chunked, which some providers reject.
	outgoing.ContentLength = int64(len(request.body))

	response, err := client.Do(outgoing)
	if err != nil {
		return nil, fmt.Errorf("upstream request to %s failed: %w", url, err)
	}
	return response, nil
}

// responseDecorator adds Caveman's own headers to a response on its way out.
type responseDecorator func(headers http.Header)

// bodyObserver is called with each chunk as it passes, and once at end of
// stream. It observes bytes that have already been forwarded, so it can neither
// delay nor alter them.
type bodyObserver interface {
	push(chunk string)
	finish()
}

func isEventStream(headers http.Header) bool {
	return strings.Contains(headers.Get("content-type"), "text/event-stream")
}

// passthroughResponse copies the upstream response to the client, forwarding
// each chunk as it arrives. An SSE stream is only a stream if every write
// reaches the client immediately, so the writer is flushed per chunk rather
// than left to fill Go's buffer.
func passthroughResponse(
	writer http.ResponseWriter,
	upstream *http.Response,
	decorate responseDecorator,
	observer bodyObserver,
) {
	headers := writer.Header()
	for name, values := range forwardableResponseHeaders(upstream.Header) {
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	if isEventStream(upstream.Header) {
		headers.Set("cache-control", "no-cache, no-transform")
		headers.Set("x-accel-buffering", "no")
	}
	if decorate != nil {
		decorate(headers)
	}
	writer.WriteHeader(upstream.StatusCode)

	flusher, _ := writer.(http.Flusher)
	buffer := make([]byte, 16*1024)
	for {
		read, readErr := upstream.Body.Read(buffer)
		if read > 0 {
			chunk := buffer[:read]
			// The chunk is written and flushed before it is observed, so a slow
			// observer cannot hold it back from the client.
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			if observer != nil {
				observer.push(string(chunk))
			}
		}
		if readErr != nil {
			break
		}
	}
	if observer != nil {
		observer.finish()
	}
}

// usageObserver watches the response for the counts the provider billed and
// reports them once the body has been delivered.
type usageObserver struct {
	observer *telemetry.UsageObserver
	reporter *telemetry.Reporter
}

func (u *usageObserver) push(chunk string) { u.observer.Push(chunk) }

func (u *usageObserver) finish() { u.reporter.RecordUsage(u.observer.Current()) }
