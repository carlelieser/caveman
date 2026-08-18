package server_test

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/carlelieser/caveman/internal/server"
	"github.com/carlelieser/caveman/internal/telemetry"
)

const streamBody = `{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"stream please"}],"stream":true}`

var sseEvents = []string{
	"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n",
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
}

var usageEvents = []string{
	"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5710,\"cache_read_input_tokens\":4200,\"cache_creation_input_tokens\":0}}}\n\n",
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n",
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":412}}\n\n",
}

// gatedStream writes each event only once the client confirms it received the
// previous one. A proxy that buffered the body would never deliver event 0, so
// the confirmation for it would never arrive and the handler would block until
// the test's timeout — which makes buffering a deterministic failure rather
// than a slow-clock guess.
func gatedStream(t *testing.T, events []string, received <-chan int) func(recordedRequest, http.ResponseWriter) {
	t.Helper()
	return func(_ recordedRequest, writer http.ResponseWriter) {
		writer.Header().Set("content-type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("fake upstream cannot flush")
			return
		}
		for index, event := range events {
			if _, err := io.WriteString(writer, event); err != nil {
				return
			}
			flusher.Flush()
			if index == len(events)-1 {
				return
			}
			select {
			case <-received:
			case <-time.After(5 * time.Second):
				t.Errorf("client never confirmed event %d; the proxy buffered the stream", index)
				return
			}
		}
	}
}

// liveProxy runs the handler on a real listener. httptest.NewRecorder buffers
// by design, so incremental delivery can only be observed over a socket.
func liveProxy(t *testing.T, upstream *fakeUpstream, options server.Options) string {
	t.Helper()
	t.Setenv("CAVEMAN_UPSTREAM_BASE_URL", upstream.BaseURL())
	proxy := httptest.NewServer(server.New(options).Handler)
	t.Cleanup(proxy.Close)
	return proxy.URL + "/v1/messages"
}

func postStream(t *testing.T, url string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(streamBody))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("content-type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	return response
}

// readEventsConfirming reads one SSE event at a time, signalling after each so
// the upstream is released to write the next.
func readEventsConfirming(t *testing.T, body io.Reader, confirm chan<- int, want int) string {
	t.Helper()
	reader := bufio.NewReader(body)
	var received strings.Builder
	for index := 0; index < want; index++ {
		// Each event ends with a blank line, so reading to the terminator reads
		// exactly one event.
		for {
			line, err := reader.ReadString('\n')
			received.WriteString(line)
			if err != nil {
				return received.String()
			}
			if line == "\n" {
				break
			}
		}
		if index < want-1 {
			select {
			case confirm <- index:
			case <-time.After(5 * time.Second):
				t.Fatalf("upstream never consumed the confirmation for event %d", index)
			}
		}
	}
	rest, _ := io.ReadAll(reader)
	received.Write(rest)
	return received.String()
}

// The proof that chunks are piped rather than buffered: upstream refuses to
// write event N+1 until the client has actually received event N.
func TestSSEDeliversChunksIncrementally(t *testing.T) {
	upstream := newFakeUpstream(t)
	received := make(chan int)
	upstream.Reply(gatedStream(t, sseEvents, received))
	url := liveProxy(t, upstream, server.Options{})

	response := postStream(t, url)
	defer response.Body.Close()

	got := readEventsConfirming(t, response.Body, received, len(sseEvents))
	if want := strings.Join(sseEvents, ""); got != want {
		t.Errorf("stream = %q, want %q", got, want)
	}
}

func TestSSEHeadersMarkTheStreamUnbuffered(t *testing.T) {
	upstream := newFakeUpstream(t)
	upstream.Reply(func(_ recordedRequest, writer http.ResponseWriter) {
		writer.Header().Set("content-type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, sseEvents[0])
	})
	url := liveProxy(t, upstream, server.Options{})

	response := postStream(t, url)
	defer response.Body.Close()
	_, _ = io.ReadAll(response.Body)

	cases := map[string]string{
		"content-type":      "text/event-stream",
		"x-accel-buffering": "no",
		"cache-control":     "no-cache, no-transform",
	}
	for name, want := range cases {
		if got := response.Header.Get(name); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", name, got, want)
		}
	}
}

// A non-streamed response must not gain the streaming headers.
func TestNonStreamResponseKeepsItsOwnHeaders(t *testing.T) {
	upstream := newFakeUpstream(t)
	handler := serve(t, upstream, server.Options{})

	recorder := post(t, handler, "/v1/messages", nil, streamBody)

	if got := recorder.Header().Get("x-accel-buffering"); got != "" {
		t.Errorf("x-accel-buffering = %q on a non-stream response", got)
	}
}

func TestSSEForwardsTheRequestBodyTransparently(t *testing.T) {
	upstream := newFakeUpstream(t)
	upstream.Reply(func(_ recordedRequest, writer http.ResponseWriter) {
		writer.Header().Set("content-type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, sseEvents[0])
	})
	url := liveProxy(t, upstream, server.Options{})

	response := postStream(t, url)
	defer response.Body.Close()
	_, _ = io.ReadAll(response.Body)

	if got := upstream.Last(t).Body; got != streamBody {
		t.Errorf("forwarded body = %q, want %q", got, streamBody)
	}
}

// Watching the bytes must cost the stream nothing: the body arrives intact and
// still incrementally while the usage observer reads the same bytes.
func TestSSEStaysIncrementalWhileUsageIsObserved(t *testing.T) {
	upstream := newFakeUpstream(t)
	received := make(chan int)
	upstream.Reply(gatedStream(t, usageEvents, received))

	lines := []string{}
	reporter := telemetry.NewReporter(func(line string) { lines = append(lines, line) })
	url := liveProxy(t, upstream, server.Options{Reporter: reporter})

	response := postStream(t, url)
	defer response.Body.Close()

	got := readEventsConfirming(t, response.Body, received, len(usageEvents))
	if want := strings.Join(usageEvents, ""); got != want {
		t.Errorf("stream = %q, want %q", got, want)
	}

	// The billed line is written from the observer's finish, which runs after
	// the body is fully delivered.
	deadline := time.After(2 * time.Second)
	for {
		billed := ""
		for _, line := range lines {
			if strings.Contains(line, "billed") {
				billed = line
			}
		}
		if billed != "" {
			for _, want := range []string{"5,710 in", "412 out", "4,200 cache read"} {
				if !strings.Contains(billed, want) {
					t.Errorf("billed line %q does not contain %q", billed, want)
				}
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("no billed line was written; saw %v", lines)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
