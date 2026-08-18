package server

import (
	"io"
	"net/http"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/telemetry"
)

// StageResult is what one compression pass produced. Stats are nil when the
// request was never compressed, so telemetry can label the response without
// re-walking it.
type StageResult struct {
	Request ir.Request
	Stats   *telemetry.PipelineStats
}

// CompressionStage is the seam the compression pipeline plugs into. The handler
// holds no knowledge of how text is compressed, only of when to ask.
type CompressionStage func(request ir.Request, p policy.Policy) StageResult

// IdentityStage forwards a request unchanged. It is the default, so a server
// built without a pipeline is a transparent proxy rather than a broken one.
func IdentityStage(request ir.Request, _ policy.Policy) StageResult {
	return StageResult{Request: request}
}

type route struct {
	provider adapters.Provider
	stage    CompressionStage
	reporter *telemetry.Reporter
	client   *http.Client
}

func (r route) reject(writer http.ResponseWriter, message string) {
	body := ir.Marshal(r.provider.ErrorEnvelope(message))
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(http.StatusBadRequest)
	_, _ = writer.Write(body)
}

// accountingDecorator labels the response with what compression did. Absent
// stats mean the request was never compressed, so nothing is reported.
func accountingDecorator(stats *telemetry.PipelineStats) responseDecorator {
	return func(headers http.Header) {
		if stats == nil {
			return
		}
		telemetry.ApplyAccountingHeaders(headers, telemetry.AccountFor(*stats))
	}
}

// observerFor watches the response for the counts the provider billed.
// Attached even when compression is off, so an uncompressed session gives a
// baseline to compare against.
func observerFor(reporter *telemetry.Reporter) bodyObserver {
	if reporter == nil {
		return nil
	}
	return &usageObserver{observer: telemetry.NewUsageObserver(), reporter: reporter}
}

func (r route) handle(writer http.ResponseWriter, request *http.Request) {
	compressionPolicy, failure := policy.Parse(request.Header)
	if failure != nil {
		r.reject(writer, failure.Error())
		return
	}

	raw, err := io.ReadAll(request.Body)
	if err != nil {
		r.reject(writer, "reading the request body failed")
		return
	}
	parsed, err := ir.Unmarshal(raw)
	if err != nil {
		r.reject(writer, "request body is not valid JSON")
		return
	}
	body, ok := parsed.(*ir.Object)
	if !ok {
		r.reject(writer, "request body is not a JSON object")
		return
	}

	staged := r.stage(r.provider.ToIR(body), compressionPolicy)

	// The client's cancellation reaches upstream, so hanging up here stops the
	// work there rather than leaving it to finish against nobody.
	upstream, err := sendUpstream(request.Context(), r.client, upstreamRequest{
		provider: r.provider,
		headers:  forwardableRequestHeaders(request.Header),
		body:     ir.MarshalString(r.provider.FromIR(staged.Request)),
		search:   incomingSearch(request),
	})
	if err != nil {
		writer.Header().Set("content-type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write(ir.Marshal(r.provider.ErrorEnvelope("upstream request failed")))
		return
	}
	defer upstream.Body.Close()

	if staged.Stats != nil && r.reporter != nil {
		r.reporter.Record(*staged.Stats)
	}
	passthroughResponse(writer, upstream, accountingDecorator(staged.Stats), observerFor(r.reporter))
}

// incomingSearch is the client's query string, forwarded so upstream sees the
// request it was addressed to.
func incomingSearch(request *http.Request) string {
	if request.URL.RawQuery == "" {
		return ""
	}
	return "?" + request.URL.RawQuery
}
