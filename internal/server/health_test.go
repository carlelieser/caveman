package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/server"
)

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestHealthAnswers200(t *testing.T) {
	recorder := get(t, server.New(server.Options{}).Handler, server.HealthPath)
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d", recorder.Code)
	}
}

// The CLI substring-matches this exact text to tell Caveman apart from a
// foreign process holding the port. Whitespace anywhere in it breaks that probe
// silently, so the body is asserted byte for byte rather than by decoding it.
func TestHealthBodyIsByteExact(t *testing.T) {
	recorder := get(t, server.New(server.Options{}).Handler, server.HealthPath)
	const want = `{"service":"caveman","status":"ok"}`
	if got := recorder.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if !strings.Contains(recorder.Body.String(), `"service":"caveman"`) {
		t.Error("body does not carry the substring the CLI probes for")
	}
}

// Readiness must not depend on any provider being registered.
func TestHealthAnswersWithNoProviders(t *testing.T) {
	handler := server.New(server.Options{Registry: adapters.Registry{}}).Handler
	if recorder := get(t, handler, server.HealthPath); recorder.Code != http.StatusOK {
		t.Errorf("status = %d with an empty registry", recorder.Code)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	if recorder := get(t, server.New(server.Options{}).Handler, "/not-a-route"); recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
}

// Health is a GET route. Answering a POST would make it a catch-all.
func TestHealthDoesNotAnswerPost(t *testing.T) {
	handler := server.New(server.Options{}).Handler
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, server.HealthPath, nil))
	if recorder.Code == http.StatusOK {
		t.Error("POST to the health path answered 200")
	}
}

func TestListenPort(t *testing.T) {
	t.Setenv("PORT", "")
	if port, err := server.ListenPort(); err != nil || port != server.DefaultPort {
		t.Errorf("empty PORT gave (%d, %v), want the default", port, err)
	}
	t.Setenv("PORT", "9999")
	if port, err := server.ListenPort(); err != nil || port != 9999 {
		t.Errorf("PORT=9999 gave (%d, %v)", port, err)
	}
	for _, bad := range []string{"not-a-port", "-1", "70000"} {
		t.Setenv("PORT", bad)
		if _, err := server.ListenPort(); err == nil {
			t.Errorf("PORT=%q was accepted", bad)
		}
	}
}
