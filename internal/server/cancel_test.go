package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/carlelieser/caveman/internal/server"
)

// The TypeScript original threaded the client's AbortSignal into the upstream
// request type but never handed it to fetch, so hanging up left the upstream
// call running to completion against a connection nobody was reading. Wiring
// the context through fixes that, and this is what proves it: upstream reports
// whether its own request context was cancelled.
func TestClientCancellationAbortsTheUpstreamRequest(t *testing.T) {
	upstream := newFakeUpstream(t)
	started := make(chan struct{})
	cancelled := make(chan bool, 1)

	upstream.ReplyLive(func(_ recordedRequest, _ http.ResponseWriter, live *http.Request) {
		// Reports what happened to the upstream connection rather than to the
		// body: its context is cancelled only if the abort actually propagated.
		close(started)
		select {
		case <-time.After(3 * time.Second):
			cancelled <- false
		case <-live.Context().Done():
			cancelled <- true
		}
	})
	url := liveProxy(t, upstream, server.Options{})

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(streamBody))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	request.Header.Set("content-type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream was never reached")
	}
	cancel()

	select {
	case aborted := <-cancelled:
		if !aborted {
			t.Error("client cancellation did not abort the upstream request")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never reported whether it was cancelled")
	}
	<-done
}
