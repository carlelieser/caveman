package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/ir"
	"github.com/carlelieser/caveman/internal/policy"
	"github.com/carlelieser/caveman/internal/server"
)

// fakeProvider is a second provider with a different route, a different wire
// format, and a different error shape. Nothing outside this file changes to
// serve it: the router reads its route from the provider, so registering it is
// the whole integration.
type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Path() string { return "/v2/chat" }

func (fakeProvider) BaseURL() string { return "https://api.fake-provider.test" }

func (fakeProvider) ToIR(body adapters.RequestBody) ir.Request {
	prompt, _ := body.Get("prompt")
	promptText, _ := prompt.(ir.String)
	model, _ := body.Get("model")
	modelText, _ := model.(ir.String)
	limit, _ := body.Get("limit")
	limitNumber, ok := limit.(ir.Number)
	if !ok {
		limitNumber = ir.NumberFromInt(0)
	}
	return ir.Request{
		Model:     string(modelText),
		MaxTokens: limitNumber,
		// No IsContentString: this provider has no string/array duality to
		// remember, and the neutral IR must not require one.
		Messages: []ir.Message{{
			Role:    ir.RoleUser,
			Content: []ir.Content{&ir.TextContent{Text: string(promptText)}},
		}},
		Passthrough: ir.NewObject(),
	}
}

func (fakeProvider) FromIR(request ir.Request) adapters.RequestBody {
	prompt := ""
	if len(request.Messages) > 0 && len(request.Messages[0].Content) > 0 {
		if text, ok := request.Messages[0].Content[0].(*ir.TextContent); ok {
			prompt = text.Text
		}
	}
	body := ir.NewObject()
	body.Set("model", ir.String(request.Model))
	body.Set("limit", request.MaxTokens)
	body.Set("prompt", ir.String(prompt))
	return body
}

func (fakeProvider) ErrorEnvelope(message string) adapters.RequestBody {
	body := ir.NewObject()
	body.Set("fault", ir.String(message))
	return body
}

var _ adapters.Provider = fakeProvider{}

const verbosePrompt = "Could you please go ahead and tell me what the weather is like in the city of " +
	"San Francisco on this particular day, if that is something you can do?"

const fakeBody = `{"model":"fake-1","limit":256,"prompt":"` + verbosePrompt + `"}`

// The route comes from the provider. A handler that hardcoded /v1/messages
// would leave this 404.
func TestServesASecondProviderOnItsOwnRoute(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{Registry: adapters.Registry{fakeProvider{}}})

	recorder := post(t, handler, "/v2/chat", nil, fakeBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := upstream.Last(t).URL; got != "/v2/chat" {
		t.Errorf("upstream saw %q, want /v2/chat", got)
	}
}

func TestServesSeveralProvidersSideBySide(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{
		Registry: adapters.Registry{anthropic.New(), fakeProvider{}},
	})

	anthropicBody := `{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	if recorder := post(t, handler, "/v1/messages", nil, anthropicBody); recorder.Code != http.StatusOK {
		t.Errorf("anthropic route status = %d", recorder.Code)
	}
	if recorder := post(t, handler, "/v2/chat", nil, fakeBody); recorder.Code != http.StatusOK {
		t.Errorf("fake route status = %d", recorder.Code)
	}

	requests := upstream.Requests()
	if len(requests) != 2 || requests[0].URL != "/v1/messages" || requests[1].URL != "/v2/chat" {
		t.Errorf("upstream saw %v", []string{requests[0].URL, requests[1].URL})
	}
}

// A route no registered provider claims is not served and never reaches
// upstream, so dropping a provider from the registry actually takes it offline.
func TestARouteNoProviderClaimsIs404(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{Registry: adapters.Registry{fakeProvider{}}})

	recorder := post(t, handler, "/v1/messages", nil, fakeBody)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
	if got := len(upstream.Requests()); got != 0 {
		t.Errorf("upstream saw %d requests for an unclaimed route", got)
	}
}

// Each provider reports failures in its own envelope. The handler generating
// the message does not know which shape it will end up in.
func TestErrorsUseTheRoutesOwnEnvelope(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{
		Registry: adapters.Registry{anthropic.New(), fakeProvider{}},
	})

	fake := post(t, handler, "/v2/chat", map[string]string{"X-Caveman-Compress": "bogus"}, fakeBody)
	if fake.Code != http.StatusBadRequest {
		t.Fatalf("fake route status = %d", fake.Code)
	}
	var fakePayload map[string]any
	if err := json.Unmarshal(fake.Body.Bytes(), &fakePayload); err != nil {
		t.Fatalf("decoding fake error: %v", err)
	}
	fault, _ := fakePayload["fault"].(string)
	if !strings.Contains(fault, "X-Caveman-Compress") {
		t.Errorf("fake fault = %q", fault)
	}
	if _, borrowed := fakePayload["error"]; borrowed {
		t.Error("the fake route borrowed the anthropic envelope")
	}

	anthropicRecorder := post(t, handler, "/v1/messages", map[string]string{"X-Caveman-Compress": "bogus"}, `{}`)
	var anthropicPayload map[string]any
	if err := json.Unmarshal(anthropicRecorder.Body.Bytes(), &anthropicPayload); err != nil {
		t.Fatalf("decoding anthropic error: %v", err)
	}
	errorField, _ := anthropicPayload["error"].(map[string]any)
	if errorField["type"] != "invalid_request_error" {
		t.Errorf("anthropic error type = %v", errorField["type"])
	}
	if _, borrowed := anthropicPayload["fault"]; borrowed {
		t.Error("the anthropic route borrowed the fake envelope")
	}
}

// The compression stage is provider-neutral: it walks the IR and never learns
// which provider produced it, so a second provider compresses through the same
// pipeline with no handler change.
func TestASecondProviderCompressesThroughTheSameStage(t *testing.T) {
	upstream := newFakeUpstream(t)
	stage := func(request ir.Request, p policy.Policy) server.StageResult {
		if !p.CompressionEnabled() {
			return server.StageResult{Request: request}
		}
		compressed := ir.MapTextNodes(request, ir.AllScopes, func(node ir.TextNode) string {
			return strings.ToUpper(node.Text)
		})
		return server.StageResult{Request: compressed}
	}
	handler := serve(t, upstream, server.Options{
		Registry: adapters.Registry{fakeProvider{}},
		Stage:    stage,
	})

	post(t, handler, "/v2/chat", map[string]string{"X-Caveman-Compress": "moderate"}, fakeBody)

	forwarded := upstream.Last(t).Body
	if !strings.Contains(forwarded, strings.ToUpper(verbosePrompt)) {
		t.Errorf("the stage did not reach the second provider's text: %s", forwarded)
	}
	// The rewrite comes back out in the second provider's own wire format.
	if !strings.HasPrefix(forwarded, `{"model":"fake-1","limit":256,"prompt":"`) {
		t.Errorf("forwarded body left the fake provider's shape: %s", forwarded)
	}
}

// Each provider forwards to its own host unless something redirects it.
func TestEachProviderRoutesToItsOwnUpstream(t *testing.T) {
	first := newFakeUpstream(t)
	second := newFakeUpstream(t)
	clearOverrides(t)
	t.Setenv("CAVEMAN_ANTHROPIC_BASE_URL", first.BaseURL())
	t.Setenv("CAVEMAN_FAKE_BASE_URL", second.BaseURL())

	handler := server.New(server.Options{
		Registry: adapters.Registry{anthropic.New(), fakeProvider{}},
	}).Handler

	anthropicBody := `{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	post(t, handler, "/v1/messages", nil, anthropicBody)
	post(t, handler, "/v2/chat", nil, fakeBody)

	firstRequests := first.Requests()
	secondRequests := second.Requests()
	if len(firstRequests) != 1 || firstRequests[0].URL != "/v1/messages" {
		t.Errorf("anthropic upstream saw %v", firstRequests)
	}
	if len(secondRequests) != 1 || secondRequests[0].URL != "/v2/chat" {
		t.Errorf("fake upstream saw %v", secondRequests)
	}
}
