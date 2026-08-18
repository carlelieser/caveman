package ir_test

import (
	"os/exec"
	"strings"
	"testing"
)

func imports(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("listing deps of %s: %v", pkg, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// The seam that keeps a second provider a drop-in: compression walks the
// neutral IR and never learns which provider produced it, and an adapter never
// learns how text is compressed. Either import would make adding a provider a
// change to the compression path.
func TestDependencyDirection(t *testing.T) {
	forbidden := map[string][]string{
		"github.com/carlelieser/caveman/internal/ir": {
			"github.com/carlelieser/caveman/internal/adapters",
			"github.com/carlelieser/caveman/internal/compress",
			"github.com/carlelieser/caveman/internal/tagger",
		},
		"github.com/carlelieser/caveman/internal/adapters": {
			"github.com/carlelieser/caveman/internal/compress",
			"github.com/carlelieser/caveman/internal/tagger",
		},
		"github.com/carlelieser/caveman/internal/adapters/anthropic": {
			"github.com/carlelieser/caveman/internal/compress",
			"github.com/carlelieser/caveman/internal/tagger",
			"github.com/carlelieser/caveman/internal/server",
		},
		// Policy names the levels and scopes; it must not learn how either is
		// applied, or the vocabulary and the mechanism move together.
		"github.com/carlelieser/caveman/internal/policy": {
			"github.com/carlelieser/caveman/internal/adapters",
			"github.com/carlelieser/caveman/internal/compress",
			"github.com/carlelieser/caveman/internal/server",
			"github.com/carlelieser/caveman/internal/tagger",
		},
		// Telemetry reports what compression did without depending on how it is
		// done, so the pipeline can change without touching the log format.
		"github.com/carlelieser/caveman/internal/telemetry": {
			"github.com/carlelieser/caveman/internal/adapters",
			"github.com/carlelieser/caveman/internal/compress",
			"github.com/carlelieser/caveman/internal/server",
			"github.com/carlelieser/caveman/internal/tagger",
		},
	}
	for pkg, banned := range forbidden {
		deps := imports(t, pkg)
		for _, dep := range deps {
			for _, ban := range banned {
				if dep == ban || strings.HasPrefix(dep, ban+"/") {
					t.Errorf("%s imports %s", pkg, dep)
				}
			}
		}
	}
}
