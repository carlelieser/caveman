package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type recordedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   string
}

// fakeUpstream stands in for the provider. It records what it was sent so the
// forwarding contract can be asserted on the bytes that actually left.
type fakeUpstream struct {
	Server      *httptest.Server
	mu          sync.Mutex
	requests    []recordedRequest
	handler     func(recordedRequest, http.ResponseWriter)
	liveHandler func(recordedRequest, http.ResponseWriter, *http.Request)
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	upstream := &fakeUpstream{}
	upstream.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		recorded := recordedRequest{
			Method: request.Method,
			URL:    request.URL.RequestURI(),
			Header: request.Header.Clone(),
			Body:   string(body),
		}
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, recorded)
		handler := upstream.handler
		liveHandler := upstream.liveHandler
		upstream.mu.Unlock()

		if liveHandler != nil {
			liveHandler(recorded, writer, request)
			return
		}
		if handler != nil {
			handler(recorded, writer)
			return
		}
		writer.Header().Set("content-type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"type":"message","content":[]}`))
	}))
	t.Cleanup(upstream.Server.Close)
	return upstream
}

func (u *fakeUpstream) Reply(handler func(recordedRequest, http.ResponseWriter)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.handler = handler
}

// ReplyLive is Reply with the live request in hand, for tests that observe the
// connection rather than the body.
func (u *fakeUpstream) ReplyLive(handler func(recordedRequest, http.ResponseWriter, *http.Request)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.liveHandler = handler
}

func (u *fakeUpstream) Requests() []recordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedRequest(nil), u.requests...)
}

func (u *fakeUpstream) Last(t *testing.T) recordedRequest {
	t.Helper()
	requests := u.Requests()
	if len(requests) == 0 {
		t.Fatal("no request reached the fake upstream")
	}
	return requests[len(requests)-1]
}

func (u *fakeUpstream) BaseURL() string { return u.Server.URL }
