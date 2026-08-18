package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/server"
)

const sampleBody = `{"model":"claude-sonnet-4-5","max_tokens":1024,"system":"You are terse.",` +
	`"messages":[{"role":"user","content":"hello"},` +
	`{"role":"assistant","content":[{"type":"text","text":"hi there"},` +
	`{"type":"tool_use","id":"tu_1","name":"search","input":{"query":"x"}}]},` +
	`{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"result text"},` +
	`{"type":"text","text":"and then?","cache_control":{"type":"ephemeral"}}]}],` +
	`"tools":[{"name":"search","description":"searches","input_schema":{"type":"object"}}],` +
	`"temperature":0.5,"metadata":{"user_id":"abc"},"future_unknown_field":{"nested":[1,2,3]}}`

// serve builds a proxy pointed at the fake upstream through the global
// override, which is how a local run redirects every provider at once.
func serve(t *testing.T, upstream *fakeUpstream, options server.Options) http.Handler {
	t.Helper()
	t.Setenv("CAVEMAN_UPSTREAM_BASE_URL", upstream.BaseURL())
	return server.New(options).Handler
}

func post(t *testing.T, handler http.Handler, path string, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("content-type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// The prompt cache matches on serialized bytes, so a forwarded body with
// reordered keys misses the cache and re-bills every cached segment even though
// its content is identical. Deep equality cannot see that; only byte equality
// can.
func TestForwardsAByteIdenticalBody(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	post(t, handler, "/v1/messages", map[string]string{"x-api-key": "sk-test"}, sampleBody)

	requests := upstream.Requests()
	if len(requests) != 1 {
		t.Fatalf("upstream saw %d requests, want 1", len(requests))
	}
	if requests[0].Body != sampleBody {
		t.Errorf("forwarded body differs\n got %s\nwant %s", requests[0].Body, sampleBody)
	}
}

func TestForwardsMethodAndPath(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	post(t, handler, "/v1/messages", nil, sampleBody)

	recorded := upstream.Last(t)
	if recorded.URL != "/v1/messages" || recorded.Method != http.MethodPost {
		t.Errorf("upstream saw %s %s", recorded.Method, recorded.URL)
	}
}

func TestForwardsTheQueryString(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	post(t, handler, "/v1/messages?beta=true", nil, sampleBody)

	if recorded := upstream.Last(t); recorded.URL != "/v1/messages?beta=true" {
		t.Errorf("upstream saw %q, want the query string forwarded", recorded.URL)
	}
}

// Credentials pass through untouched and are never read.
func TestForwardsAuthAndVersionHeadersVerbatim(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	sent := map[string]string{
		"x-api-key":         "sk-ant-secret",
		"authorization":     "Bearer token-value",
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    "prompt-caching-2024-07-31",
	}
	post(t, handler, "/v1/messages", sent, sampleBody)

	recorded := upstream.Last(t)
	for name, want := range sent {
		if got := recorded.Header.Get(name); got != want {
			t.Errorf("%s forwarded as %q, want %q", name, got, want)
		}
	}
}

// A control header that reaches the provider is a leak of Caveman's own
// protocol into someone else's API.
func TestStripsEveryCavemanHeader(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	post(t, handler, "/v1/messages", map[string]string{
		"x-api-key":          "sk-test",
		"X-Caveman-Compress": "off",
		"X-Caveman-Scope":    "messages",
		"X-Caveman-Cache":    "ignore",
	}, sampleBody)

	for name := range upstream.Last(t).Header {
		if strings.HasPrefix(strings.ToLower(name), "x-caveman-") {
			t.Errorf("forwarded control header %q", name)
		}
	}
}

// A stale content-length disagrees with the re-serialized body, and host points
// at Caveman rather than the provider.
func TestRecomputesContentLengthAndHost(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	recorder := post(t, handler, "/v1/messages", map[string]string{
		"x-api-key":      "sk-test",
		"content-length": "999999",
		"host":           "localhost:8787",
	}, sampleBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	recorded := upstream.Last(t)
	want := strconv.Itoa(len(recorded.Body))
	if got := recorded.Header.Get("content-length"); got != "" && got != want {
		t.Errorf("content-length = %q, want %q", got, want)
	}
	if strings.Contains(recorded.Header.Get("host"), "8787") {
		t.Errorf("forwarded Caveman's own host: %q", recorded.Header.Get("host"))
	}
}

// Decoding is left to the transport so an SSE stream arrives as plain text
// chunks rather than a compressed frame that would batch tokens.
func TestDoesNotForwardAcceptEncoding(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	post(t, handler, "/v1/messages", map[string]string{"accept-encoding": "gzip, br"}, sampleBody)

	if got := upstream.Last(t).Header.Get("accept-encoding"); strings.Contains(got, "br") {
		t.Errorf("accept-encoding forwarded as %q", got)
	}
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding error body %q: %v", recorder.Body.String(), err)
	}
	return payload
}

// A rejected request must never reach the provider: it would be billed for a
// request Caveman knows it cannot honour.
func TestPolicyFailuresAreRejectedBeforeUpstream(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		names   string
	}{
		{"unknown level", map[string]string{"X-Caveman-Compress": "not-a-number"}, "X-Caveman-Compress"},
		{"fractional level", map[string]string{"X-Caveman-Compress": "0.5"}, "X-Caveman-Compress"},
		{"unknown scope", map[string]string{"X-Caveman-Scope": "messages,nonsense"}, "X-Caveman-Scope"},
		{"unknown cache mode", map[string]string{"X-Caveman-Cache": "maybe"}, "X-Caveman-Cache"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			upstream := newFakeUpstream(t)
			handler := serve(t, upstream, server.Options{})

			recorder := post(t, handler, "/v1/messages", test.headers, sampleBody)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", recorder.Code)
			}
			if got := len(upstream.Requests()); got != 0 {
				t.Errorf("upstream saw %d requests for a rejected policy", got)
			}
			payload := decodeError(t, recorder)
			errorField, _ := payload["error"].(map[string]any)
			message, _ := errorField["message"].(string)
			if !strings.Contains(message, test.names) {
				t.Errorf("message %q does not name %s", message, test.names)
			}
			if errorField["type"] != "invalid_request_error" {
				t.Errorf("error type = %v", errorField["type"])
			}
		})
	}
}

func TestMalformedJSONIsRejectedBeforeUpstream(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	recorder := post(t, handler, "/v1/messages", nil, "{ not json")

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
	if got := len(upstream.Requests()); got != 0 {
		t.Errorf("upstream saw %d requests for a malformed body", got)
	}
	errorField, _ := decodeError(t, recorder)["error"].(map[string]any)
	if errorField["type"] != "invalid_request_error" {
		t.Errorf("error type = %v", errorField["type"])
	}
}

// An upstream failure is the provider's answer, not Caveman's, so status and
// body pass through as they arrived.
func TestUpstreamStatusAndBodyPassThrough(t *testing.T) {
	cases := []struct {
		status int
		body   string
	}{
		{http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`},
		{http.StatusInternalServerError, `{"type":"error","error":{"type":"api_error","message":"upstream exploded"}}`},
		{http.StatusOK, `{"type":"message","id":"msg_1","content":[{"type":"text","text":"ok"}]}`},
	}
	for _, test := range cases {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			upstream := newFakeUpstream(t)
			upstream.Reply(func(_ recordedRequest, writer http.ResponseWriter) {
				writer.Header().Set("content-type", "application/json")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			})
			handler := serve(t, upstream, server.Options{})

			recorder := post(t, handler, "/v1/messages", nil, sampleBody)

			if recorder.Code != test.status {
				t.Errorf("status = %d, want %d", recorder.Code, test.status)
			}
			if recorder.Body.String() != test.body {
				t.Errorf("body = %q, want %q", recorder.Body.String(), test.body)
			}
			if got := recorder.Header().Get("content-type"); !strings.Contains(got, "application/json") {
				t.Errorf("content-type = %q", got)
			}
		})
	}
}
