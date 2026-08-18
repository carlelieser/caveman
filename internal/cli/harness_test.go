package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// binaryPath is the compiled CLI. The daemon paths re-execute the binary, so the
// tests drive the real thing rather than an in-process handler.
var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "caveman-cli-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "caveman")
	build := exec.Command("go", "build", "-o", binaryPath, "github.com/carlelieser/caveman/cmd/caveman")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code   int
	stdout string
	stderr string
}

func (r result) String() string {
	return fmt.Sprintf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
}

// harness is one throwaway run directory and the environment pointing at it.
type harness struct {
	t      *testing.T
	runDir string
	env    map[string]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{t: t, runDir: t.TempDir(), env: map[string]string{}}
}

func (h *harness) with(key, value string) *harness {
	h.env[key] = value
	return h
}

// run never fails the test on a non-zero exit: the code is the assertion target.
func (h *harness) run(args ...string) result {
	h.t.Helper()
	return h.runWith(nil, args...)
}

func (h *harness) runWith(extra map[string]string, args ...string) result {
	h.t.Helper()
	command := exec.Command(binaryPath, args...)
	environment := append(os.Environ(),
		"CAVEMAN_RUN_DIR="+h.runDir,
		// Never the developer's own install root.
		"CAVEMAN_HOME="+h.runDir,
		"CAVEMAN_SERVE=",
	)
	for key, value := range h.env {
		environment = append(environment, key+"="+value)
	}
	for key, value := range extra {
		environment = append(environment, key+"="+value)
	}
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			code = exitErr.ExitCode()
		} else {
			h.t.Fatalf("running %v: %v", args, err)
		}
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func (h *harness) path(name string) string { return filepath.Join(h.runDir, name) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// freePort binds and releases, so the number is one nothing currently holds.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return strconv.Itoa(port)
}

// startForeign holds the port with a server that answers 200 without the
// caveman marker.
func startForeign(t *testing.T, port string, body string) *http.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(body))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	waitForPort(t, port)
	return server
}

func waitForPort(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %s", port)
}

// stopOnCleanup guarantees no daemon survives a test, however it exited.
func (h *harness) stopOnCleanup(port string) {
	h.t.Cleanup(func() {
		h.runWith(map[string]string{"PORT": port}, "down")
	})
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requireContains(t *testing.T, got, want, label string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("%s: expected %q in %q", label, want, got)
	}
}

func requireCode(t *testing.T, got result, want int) {
	t.Helper()
	if got.code != want {
		t.Fatalf("expected exit %d, got %s", want, got)
	}
}
