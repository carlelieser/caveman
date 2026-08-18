package cli_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// healthBody is what the probe substring-matches. Written literally: the CLI
// tells its own server apart from an unrelated one by these exact bytes.
const healthBody = `{"service":"caveman","status":"ok"}`

func TestHealthAnswersTheExactByteString(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.stopOnCleanup(port)

	requireCode(t, harness.run("up"), 0)
	body := getBody(t, "http://127.0.0.1:"+port+"/health")
	if body != healthBody {
		t.Fatalf("health body\n got %q\nwant %q", body, healthBody)
	}
}

func TestA200WithoutTheMarkerIsForeignNotCaveman(t *testing.T) {
	port := freePort(t)
	// Same status field, no service field: a 200 alone must not read as ours.
	startForeign(t, port, `{"status":"ok"}`)

	harness := newHarness(t).with("PORT", port)
	status := harness.run("status")
	requireCode(t, status, 1)
	requireContains(t, status.stdout, "not running", "foreign status")

	up := harness.run("up")
	requireCode(t, up, 3)
}

func TestANonTwoHundredAnswerIsForeign(t *testing.T) {
	port := freePort(t)
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	waitForPort(t, port)

	requireCode(t, newHarness(t).with("PORT", port).run("up"), 3)
}

// A daemon that dies on boot must be reported as such, promptly. Reaching the
// readiness deadline instead would mean the CLI is watching an unreaped zombie,
// which answers `kill -0` long after the process is gone.
func TestAServerThatDiesOnBootIsReportedNotWaitedOut(t *testing.T) {
	port := freePort(t)
	holdDualStack(t, port)

	started := time.Now()
	result := newHarness(t).with("PORT", port).run("up")
	elapsed := time.Since(started)

	requireCode(t, result, 4)
	requireContains(t, result.stderr, "the server exited during startup", "boot crash")
	// The log tail is what names the cause, so it must accompany the message.
	requireContains(t, result.stderr, "caveman.log", "log tail")
	if elapsed > 10*time.Second {
		t.Fatalf("took %s to notice a boot crash; the readiness budget was waited out", elapsed)
	}
}

// holdDualStack takes the port on both stacks, so the daemon's own listener
// cannot bind. A v4-only holder would leave the v6 wildcard free and the daemon
// would start successfully alongside it.
func holdDualStack(t *testing.T, port string) {
	t.Helper()
	config := net.ListenConfig{}
	listener, err := config.Listen(context.Background(), "tcp", "[::]:"+port)
	if err != nil {
		t.Skipf("cannot hold %s on both stacks: %v", port, err)
	}
	t.Cleanup(func() { listener.Close() })
}

// fakeUpstream answers /v1/messages so a request can flow through the daemon and
// leave the reporter with something to summarize.
func fakeUpstream(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant",` +
			`"content":[{"type":"text","text":"ok"}],` +
			`"usage":{"input_tokens":10,"output_tokens":2}}`))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return "http://" + listener.Addr().String()
}

const messagesBody = `{"model":"claude-3-5-sonnet","max_tokens":16,"messages":[` +
	`{"role":"user","content":"The man has quickly gone to the very large store"}]}`

// sendCompressed drives one request through the running daemon so the session
// summary has something to report.
func sendCompressed(t *testing.T, port string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost,
		"http://127.0.0.1:"+port+"/v1/messages", strings.NewReader(messagesBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("X-Caveman-Compress", "caveman")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxying failed: %d", response.StatusCode)
	}
}

// TestDownTerminatesGracefullySoTheSummarySurvives is the SIGTERM-before-SIGKILL
// gate: a hard kill skips the summary, so its presence is what proves the order.
func TestDownTerminatesGracefullySoTheSummarySurvives(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_ANTHROPIC_BASE_URL", fakeUpstream(t))
	harness.stopOnCleanup(port)

	requireCode(t, harness.run("up"), 0)
	sendCompressed(t, port)

	stopped := harness.run("down")
	requireCode(t, stopped, 0)
	requireContains(t, stopped.stdout, "caveman stopped", "down")
	requireContains(t, stopped.stdout, "caveman  session  ", "session summary")
	requireContains(t, stopped.stdout, "across 1 request", "session summary")
	if strings.Contains(stopped.stderr, "ignored SIGTERM") {
		t.Fatalf("expected a graceful stop, got %q", stopped.stderr)
	}
}

// The separators are part of the contract: two spaces between fields, U+2192
// between the counts, en-US grouping on the numbers.
func TestTheLogCarriesTheSeparatorsTheContractNames(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_ANTHROPIC_BASE_URL", fakeUpstream(t))
	harness.stopOnCleanup(port)

	requireCode(t, harness.run("up"), 0)
	sendCompressed(t, port)
	harness.run("down")

	log, err := os.ReadFile(harness.path("caveman.log"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(log)
	// The request line: two spaces between every field, U+2192 between the
	// counts, U+2014 before the running total.
	// Counting is opt-in, so an ordinary run reports characters. The unit is the
	// only part of the shape that moves.
	requestLine := regexp.MustCompile(
		`(?m)^caveman  [\d,]+ → [\d,]+ (tok|char)  -[\d.]+%  \w+  .+  \d+% prose  —  session [\d,]+ saved$`)
	if !requestLine.MatchString(contents) {
		t.Fatalf("request line does not match the contract in:\n%s", contents)
	}
	// The billed line uses U+2014 for a count the response did not carry.
	billedLine := regexp.MustCompile(`(?m)^caveman  billed  .*— cache read  — cache write$`)
	if !billedLine.MatchString(contents) {
		t.Fatalf("billed line does not match the contract in:\n%s", contents)
	}
	summaryLine := regexp.MustCompile(`(?m)^caveman  session  [\d,]+ (tok|char) saved across 1 request$`)
	if !summaryLine.MatchString(contents) {
		t.Fatalf("session summary does not match the contract in:\n%s", contents)
	}
}

func TestTheLogGroupsThousandsInEnglish(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_ANTHROPIC_BASE_URL", fakeUpstream(t))
	harness.stopOnCleanup(port)

	requireCode(t, harness.run("up"), 0)
	// Long enough that the token counts pass a thousand and must be grouped.
	sendLarge(t, port)
	harness.run("down")

	log, err := os.ReadFile(harness.path("caveman.log"))
	if err != nil {
		t.Fatal(err)
	}
	assertGrouped(t, string(log))
}

// groupedCount is a token count with en-US grouping: 7,800 rather than 7800. It
// is anchored to the fields that carry counts, since the node list has a comma
// of its own that would otherwise pass for grouping.
var groupedCount = regexp.MustCompile(`(?m)^caveman  \d{1,3}(,\d{3})+ → \d{1,3}(,\d{3})* (tok|char)  `)

var groupedSummary = regexp.MustCompile(`(?m)^caveman  session  \d{1,3}(,\d{3})+ (tok|char) saved `)

func assertGrouped(t *testing.T, log string) {
	t.Helper()
	// Nothing to prove if the run stayed under a thousand tokens.
	if !regexp.MustCompile(`(?m)^caveman  \d{4,} → `).MatchString(log) &&
		!groupedCount.MatchString(log) {
		t.Fatalf("expected a request line with a four-figure count in:\n%s", log)
	}
	if !groupedCount.MatchString(log) {
		t.Fatalf("request line is not grouped en-US in:\n%s", log)
	}
	if !groupedSummary.MatchString(log) {
		t.Fatalf("session summary is not grouped en-US in:\n%s", log)
	}
}

func sendLarge(t *testing.T, port string) {
	t.Helper()
	sentence := "The man has quickly gone to the very large store and the woman was there too. "
	text := strings.Repeat(sentence, 400)
	body := `{"model":"claude-3-5-sonnet","max_tokens":16,"messages":[` +
		`{"role":"user","content":"` + text + `"}]}`
	request, err := http.NewRequest(http.MethodPost,
		"http://127.0.0.1:"+port+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("X-Caveman-Compress", "caveman")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxying failed: %d", response.StatusCode)
	}
}

// At `off` the CLI sends no header at all, so the request forwards
// byte-identical rather than carrying a level that means "do nothing".
func TestOffSendsNoHeaderAtAll(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "off")
	result := harness.run("show")
	requireContains(t, result.stdout, "level=off", "off level")
	requireContains(t, result.stdout, "header=\n", "off header")
}

func TestANonOffLevelSendsTheHeader(t *testing.T) {
	harness, _ := levelHarness(t)
	harness.run("up", "-l", "moderate")
	result := harness.run("show")
	requireContains(t, result.stdout, "header=X-Caveman-Compress: moderate", "header")
}

// The run directory is where the three files live, under CAVEMAN_RUN_DIR.
func TestTheRunDirectoryHoldsTheThreeFiles(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.stopOnCleanup(port)

	requireCode(t, harness.run("up", "-l", "light"), 0)
	for _, name := range []string{"caveman.pid", "caveman.log", "caveman.level"} {
		if !exists(harness.path(name)) {
			t.Fatalf("expected %s in the run directory", name)
		}
	}
	level, err := os.ReadFile(harness.path("caveman.level"))
	if err != nil {
		t.Fatal(err)
	}
	if string(level) != "light" {
		t.Fatalf("stored level: got %q", level)
	}
}

// A client that is not on PATH exits 127 rather than pretending it launched.
func TestAMissingClientExits127(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("PATH", t.TempDir())
	harness.stopOnCleanup(port)

	result := harness.run("claude")
	requireCode(t, result, 127)
	requireContains(t, result.stderr, "not found on PATH", "missing client")
}

// A file in the client directory shadows the builtin of the same name, which is
// what keeps the drop-in contract meaningful.
func TestADroppedFileShadowsTheBuiltin(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_CLIENT_DIR", clientDir(t, "claude", echoerScript))
	harness.stopOnCleanup(port)

	harness.run("up")
	result := harness.run("claude", "--resume")
	requireContains(t, result.stdout, "url=http://localhost:"+port, "shadowed client")
	requireContains(t, result.stdout, "args=--resume", "shadowed client")
}

// A client launch starts the server when nothing is up, and says so once.
func TestAClientLaunchStartsTheServer(t *testing.T) {
	harness := newHarness(t)
	port := freePort(t)
	harness.with("PORT", port)
	harness.with("CAVEMAN_CLIENT_DIR", clientDir(t, "echoer", echoerScript))
	harness.stopOnCleanup(port)

	result := harness.run("echoer")
	requireContains(t, result.stdout, "caveman listening on", "implicit start")
	requireContains(t, result.stdout, "url=http://localhost:"+port, "client env")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if getBody(t, "http://127.0.0.1:"+port+"/health") == healthBody {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected the server left running after a client launch")
}
