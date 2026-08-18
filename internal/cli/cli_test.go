package cli_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsageListsTheClientsItFound(t *testing.T) {
	result := newHarness(t).run("help")
	requireCode(t, result, 0)
	requireContains(t, result.stdout, "claude", "usage")
}

func TestNoCommandExitsTwo(t *testing.T) {
	requireCode(t, newHarness(t).run(), 2)
}

func TestUnknownCommandIsNamed(t *testing.T) {
	result := newHarness(t).run("nope")
	requireCode(t, result, 2)
	requireContains(t, result.stderr, "nope", "unknown command")
}

func TestPortPrefersTheEnvironment(t *testing.T) {
	result := newHarness(t).with("PORT", "9393").run("status")
	requireContains(t, result.stdout, "9393", "status")
}

func TestOutOfRangePortReportsTheServersMessage(t *testing.T) {
	result := newHarness(t).with("PORT", "99999").run("status")
	requireCode(t, result, 1)
	requireContains(t, result.stderr, "reading PORT failed", "port failure")
	requireContains(t, result.stderr, "99999", "port failure")
}

func TestNonNumericPortReportsTheServersMessage(t *testing.T) {
	result := newHarness(t).with("PORT", "abc").run("status")
	requireCode(t, result, 1)
	requireContains(t, result.stderr, "reading PORT failed", "port failure")
}

func TestStartsReportsStatusAndStops(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.stopOnCleanup(port)

	started := harness.run("up")
	requireCode(t, started, 0)
	requireContains(t, started.stdout, port, "up")
	if !exists(harness.path("caveman.pid")) {
		t.Fatal("expected a pid file after up")
	}

	status := harness.run("status")
	requireCode(t, status, 0)
	requireContains(t, status.stdout, "running", "status")

	stopped := harness.run("down")
	requireCode(t, stopped, 0)
	if exists(harness.path("caveman.pid")) {
		t.Fatal("expected the pid file gone after down")
	}
}

func TestReportsAnAlreadyRunningServer(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.stopOnCleanup(port)

	harness.run("up")
	first, err := os.ReadFile(harness.path("caveman.pid"))
	if err != nil {
		t.Fatal(err)
	}

	again := harness.run("up")
	requireCode(t, again, 0)
	requireContains(t, again.stdout, "already running", "second up")

	second, err := os.ReadFile(harness.path("caveman.pid"))
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("pid changed: %q then %q", first, second)
	}
}

func TestStoppingWhatIsNotRunningExitsZero(t *testing.T) {
	result := newHarness(t).with("PORT", freePort(t)).run("down")
	requireCode(t, result, 0)
	requireContains(t, result.stdout, "not running", "down")
}

func TestClearsAStalePIDFile(t *testing.T) {
	harness := newHarness(t).with("PORT", freePort(t))
	if err := os.WriteFile(harness.path("caveman.pid"), []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := harness.run("status")
	requireContains(t, status.stdout, "not running", "status")

	stopped := harness.run("down")
	requireCode(t, stopped, 0)
	if exists(harness.path("caveman.pid")) {
		t.Fatal("expected the stale pid file removed")
	}
}

func TestRefusesToStartWhenTheAnswerIsNotCaveman(t *testing.T) {
	port := freePort(t)
	startForeign(t, port, `{"status":"ok"}`)

	result := newHarness(t).with("PORT", port).run("up")
	requireCode(t, result, 3)
	requireContains(t, result.stderr, port, "port taken")
}

func TestLeavesTheForeignProcessAlive(t *testing.T) {
	port := freePort(t)
	startForeign(t, port, `{"status":"ok"}`)

	newHarness(t).with("PORT", port).run("up")

	// Its own body is the proof it was neither closed nor replaced.
	body := getBody(t, "http://127.0.0.1:"+port+"/health")
	if body != `{"status":"ok"}` {
		t.Fatalf("foreign server changed: %q", body)
	}
}

// clientDir writes a drop-in client and points the CLI at its directory.
func clientDir(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

const echoerScript = `#!/bin/sh
printf "url=%s args=%s\n" "$CAVEMAN_BASE_URL" "$*"
`

const reporterScript = `#!/bin/sh
printf "level=%s args=%s header=%s\n" "$CAVEMAN_LEVEL" "$*" "$CAVEMAN_COMPRESS_HEADER"
`

func TestPassesTheBaseURLAndEveryArgumentToTheClient(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_CLIENT_DIR", clientDir(t, "echoer", echoerScript))
	harness.stopOnCleanup(port)

	harness.run("up")
	result := harness.run("echoer", "--resume", "-p", "hi there")

	requireContains(t, result.stdout, "url=http://localhost:"+port, "client env")
	requireContains(t, result.stdout, "args=--resume -p hi there", "client args")
}

func TestListsAClientDroppedIntoTheDirectory(t *testing.T) {
	harness := newHarness(t).with("CAVEMAN_CLIENT_DIR", clientDir(t, "codex", echoerScript))
	result := harness.run("help")
	requireContains(t, result.stdout, "codex", "usage")
}

// levelHarness is a run directory with a reporting client installed.
func levelHarness(t *testing.T) (*harness, string) {
	t.Helper()
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_CLIENT_DIR", clientDir(t, "show", reporterScript))
	harness.stopOnCleanup(port)
	return harness, port
}

func TestRejectsALevelThatIsNotOneOfTheFour(t *testing.T) {
	harness, _ := levelHarness(t)
	result := harness.run("up", "--level", "bogus")
	requireCode(t, result, 2)
	requireContains(t, result.stderr, "bogus", "level rejection")
}

func TestRejectsTheFlagWithNoLevelAfterIt(t *testing.T) {
	harness, _ := levelHarness(t)
	requireCode(t, harness.run("up", "--level"), 2)
}

func TestCompressesWhenNoLevelWasGiven(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up")
	requireContains(t, harness.run("show").stdout, "level=caveman", "default level")
}

func TestStillAllowsOffExplicitly(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "off")
	requireContains(t, harness.run("show").stdout, "level=off", "off level")
}

func TestInheritsTheLevelGivenToUp(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "moderate")
	requireContains(t, harness.run("show").stdout, "level=moderate", "inherited level")
}

func TestReportsTheStoredLevelInStatus(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "light")
	requireContains(t, harness.run("status").stdout, "level: light", "status level")
}

func TestClientOverridesTheStoredLevelForThatLaunchOnly(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "moderate")
	requireContains(t, harness.run("show", "-l", "caveman").stdout, "level=caveman", "override")
	requireContains(t, harness.run("show").stdout, "level=moderate", "after override")
}

func TestAcceptsLevelEqualsValue(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "--level=caveman")
	requireContains(t, harness.run("show").stdout, "level=caveman", "--level=value")
}

func TestKeepsTheLevelFlagOutOfTheClientArguments(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up")
	result := harness.run("show", "-l", "light", "--resume", "-p", "hi there")
	requireContains(t, result.stdout, "args=--resume -p hi there", "stripped flag")
}

func TestLeavesAClientsOwnDashLAloneAfterDoubleDash(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "light")
	result := harness.run("show", "--", "-l", "something")
	requireContains(t, result.stdout, "level=light", "level after --")
	requireContains(t, result.stdout, "args=-l something", "args after --")
}

// Version is what a bug report identifies a build by. It is stamped with
// -ldflags at release; a build made any other way says "dev" rather than
// reporting nothing at all.
func TestVersionReportsTheBuild(t *testing.T) {
	for _, command := range []string{"version", "--version", "-v"} {
		result := newHarness(t).run(command)
		requireCode(t, result, 0)
		requireContains(t, result.stdout, "caveman", "dev")
	}
}
