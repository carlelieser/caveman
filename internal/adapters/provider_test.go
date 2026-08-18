package adapters_test

import (
	"testing"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/ir"
)

// fakeProvider is a second provider with a different route, a different wire
// format, and a different error shape. It exists to prove the layers above hold
// no knowledge of any one provider: nothing outside this file changes to add
// it, and nothing in the compression path knows it exists.
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

const verbosePrompt = "Could you please go ahead and tell me what the weather is like in the city of " +
	"San Francisco on this particular day, if that is something you can do?"

func fakeBody() adapters.RequestBody {
	body := ir.NewObject()
	body.Set("model", ir.String("fake-1"))
	body.Set("limit", ir.NumberFromInt(256))
	body.Set("prompt", ir.String(verbosePrompt))
	return body
}

func TestASecondProviderNeedsNoChangesElsewhere(t *testing.T) {
	registry := adapters.Registry{anthropic.New(), fakeProvider{}}

	provider, ok := registry.ByPath("/v2/chat")
	if !ok {
		t.Fatal("the registry did not serve the second provider's route")
	}
	if provider.Name() != "fake" {
		t.Errorf("route /v2/chat resolved to %q", provider.Name())
	}
	if provider.BaseURL() != "https://api.fake-provider.test" {
		t.Errorf("second provider forwards to %q", provider.BaseURL())
	}
}

func TestEachProviderKeepsItsOwnRoute(t *testing.T) {
	registry := adapters.Registry{anthropic.New(), fakeProvider{}}
	cases := map[string]string{"/v1/messages": "anthropic", "/v2/chat": "fake"}
	for path, want := range cases {
		provider, ok := registry.ByPath(path)
		if !ok {
			t.Fatalf("no provider claimed %s", path)
		}
		if provider.Name() != want {
			t.Errorf("%s resolved to %q, want %q", path, provider.Name(), want)
		}
	}
}

func TestARouteNoProviderClaimsIsNotServed(t *testing.T) {
	registry := adapters.Registry{fakeProvider{}}
	if _, ok := registry.ByPath("/v1/messages"); ok {
		t.Error("a route no registered provider claims was served")
	}
}

// Each provider reports failures in its own shape. The layer that generates the
// message does not know which shape it will end up in.
func TestErrorsUseTheProvidersOwnEnvelope(t *testing.T) {
	message := "X-Caveman-Compress must name a known level"

	fake := ir.MarshalString(fakeProvider{}.ErrorEnvelope(message))
	if fake != `{"fault":"X-Caveman-Compress must name a known level"}` {
		t.Errorf("fake envelope = %s", fake)
	}

	envelope := anthropic.New().ErrorEnvelope(message)
	inner, ok := envelope.Get("error")
	if !ok {
		t.Fatal("the anthropic envelope carried no error field")
	}
	errorType, _ := inner.(*ir.Object).Get("type")
	if errorType != ir.String("invalid_request_error") {
		t.Errorf("anthropic error type = %v", errorType)
	}
	if envelope.Has("fault") {
		t.Error("the anthropic envelope borrowed the other provider's shape")
	}
}

// The neutral IR is what the compression pipeline walks. A provider that models
// far less than Anthropic must still produce nodes the same walk can find,
// which is what keeps compression provider-agnostic.
func TestTheNeutralIRCarriesASecondProvidersText(t *testing.T) {
	request := fakeProvider{}.ToIR(fakeBody())
	nodes := ir.CollectTextNodes(request, ir.AllScopes)
	if len(nodes) != 1 {
		t.Fatalf("walked %d text nodes, want 1", len(nodes))
	}
	if nodes[0].Text != verbosePrompt {
		t.Errorf("walked text = %q", nodes[0].Text)
	}
	if nodes[0].Role != ir.RoleUser {
		t.Errorf("walked role = %q", nodes[0].Role)
	}
}

// A rewrite applied through the neutral walk must come back out in the second
// provider's own wire format, with no Anthropic-shaped keys anywhere in it.
func TestASecondProviderRoundTripsThroughTheSameWalk(t *testing.T) {
	provider := fakeProvider{}
	request := provider.ToIR(fakeBody())
	compressed := ir.MapTextNodes(request, ir.AllScopes, func(ir.TextNode) string {
		return "weather SF today?"
	})
	got := ir.MarshalString(provider.FromIR(compressed))
	want := `{"model":"fake-1","limit":256,"prompt":"weather SF today?"}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// An IR built without IsContentString is the block-array form. The neutral IR
// must not require a flag that only one provider's wire format needs.
func TestAnOmittedContentStringFlagMeansTheBlockArrayForm(t *testing.T) {
	value, err := ir.Unmarshal([]byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	request := anthropic.New().ToIR(value.(*ir.Object))
	request.Messages[0].IsContentString = false

	body := anthropic.New().FromIR(request)
	messages, _ := body.Get("messages")
	content, _ := messages.(ir.Array)[0].(*ir.Object).Get("content")
	if _, ok := content.(ir.Array); !ok {
		t.Errorf("content came back as %T, want the block-array form", content)
	}
}

var _ adapters.Provider = fakeProvider{}
