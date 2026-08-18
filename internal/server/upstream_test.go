package server_test

import (
	"testing"

	"github.com/carlelieser/caveman/internal/adapters/anthropic"
	"github.com/carlelieser/caveman/internal/server"
)

// clearOverrides makes each case start from no override at all, since t.Setenv
// only restores what it set.
func clearOverrides(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CAVEMAN_UPSTREAM_BASE_URL",
		"CAVEMAN_ANTHROPIC_BASE_URL",
		"CAVEMAN_FAKE_BASE_URL",
	} {
		t.Setenv(name, "")
	}
}

func TestBaseURLDefaultsToTheProvidersOwn(t *testing.T) {
	clearOverrides(t)
	if got := server.UpstreamBaseURL(anthropic.New()); got != "https://api.anthropic.com" {
		t.Errorf("anthropic base url = %q", got)
	}
	if got := server.UpstreamBaseURL(fakeProvider{}); got != "https://api.fake-provider.test" {
		t.Errorf("fake base url = %q", got)
	}
	if server.UpstreamBaseURL(anthropic.New()) == server.UpstreamBaseURL(fakeProvider{}) {
		t.Error("both providers resolved to the same host by default")
	}
}

func TestGlobalOverrideAppliesToEveryProvider(t *testing.T) {
	clearOverrides(t)
	t.Setenv("CAVEMAN_UPSTREAM_BASE_URL", "http://localhost:9999")
	for _, got := range []string{
		server.UpstreamBaseURL(anthropic.New()),
		server.UpstreamBaseURL(fakeProvider{}),
	} {
		if got != "http://localhost:9999" {
			t.Errorf("base url = %q, want the global override", got)
		}
	}
}

// A per-provider override is how one provider is redirected without moving the
// others.
func TestProviderOverrideBeatsTheGlobalOne(t *testing.T) {
	clearOverrides(t)
	t.Setenv("CAVEMAN_UPSTREAM_BASE_URL", "http://localhost:9999")
	t.Setenv("CAVEMAN_FAKE_BASE_URL", "http://localhost:8888")

	if got := server.UpstreamBaseURL(fakeProvider{}); got != "http://localhost:8888" {
		t.Errorf("fake base url = %q, want its own override", got)
	}
	if got := server.UpstreamBaseURL(anthropic.New()); got != "http://localhost:9999" {
		t.Errorf("anthropic base url = %q, want the global override", got)
	}
}

// The variable name is derived from the provider name, so a new provider gets
// its override for free.
func TestOverrideNameIsDerivedFromTheProviderName(t *testing.T) {
	clearOverrides(t)
	t.Setenv("CAVEMAN_ANTHROPIC_BASE_URL", "http://localhost:7777")
	if got := server.UpstreamBaseURL(anthropic.New()); got != "http://localhost:7777" {
		t.Errorf("anthropic base url = %q", got)
	}
}

func TestTrailingSlashesAreTrimmed(t *testing.T) {
	clearOverrides(t)
	t.Setenv("CAVEMAN_FAKE_BASE_URL", "http://localhost:8888///")
	if got := server.UpstreamBaseURL(fakeProvider{}); got != "http://localhost:8888" {
		t.Errorf("base url = %q, want the path not doubled", got)
	}
}

// A whitespace-only override would otherwise forward to a bare path.
func TestBlankOverrideIsIgnored(t *testing.T) {
	clearOverrides(t)
	t.Setenv("CAVEMAN_FAKE_BASE_URL", "   ")
	if got := server.UpstreamBaseURL(fakeProvider{}); got != "https://api.fake-provider.test" {
		t.Errorf("base url = %q, want the provider default", got)
	}
}
