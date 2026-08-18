package cli

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	termTimeout  = 10 * time.Second
	stopInterval = 100 * time.Millisecond
)

// summaryPrefix matches the session total, not the per-request line that also
// says "session".
const summaryPrefix = "caveman  session  "

// serveEnvVar tells the re-executed binary to run the server instead of
// dispatching a subcommand. The daemon is this same binary, so `up` spawns
// itself rather than an interpreter and a script.
const serveEnvVar = "CAVEMAN_SERVE"

func formatSeconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10) + "s"
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// readPID returns the recorded pid only when that process is still alive, so a
// leftover file never reads as running.
func (c *CLI) readPID() (int, bool) {
	contents, err := os.ReadFile(c.paths.PIDFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

func (c *CLI) clearStalePID() {
	if _, alive := c.readPID(); !alive {
		os.Remove(c.paths.PIDFile)
	}
}

// spawned is the launched daemon: its pid, and a channel that closes when it
// exits. The channel is what makes a boot crash visible — the child is ours
// until it is reaped, and an unreaped zombie still answers `kill -0`, so
// liveness alone would wait out the whole readiness budget.
type spawned struct {
	pid    int
	exited <-chan struct{}
}

// spawnServer re-executes this binary in serve mode, detached, with its output
// appended to the log.
func (c *CLI) spawnServer(port int) (spawned, *exitError) {
	if failure := c.paths.ensureRunDir(); failure != nil {
		return spawned{}, failure
	}
	log, err := os.OpenFile(c.paths.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return spawned{}, die(ExitFailure, "opening %s failed", c.paths.LogFile)
	}
	defer log.Close()

	stamp := c.now().Format("2006-01-02T15:04:05")
	if _, err := log.WriteString("--- started " + stamp + " ---\n"); err != nil {
		return spawned{}, die(ExitFailure, "writing to %s failed", c.paths.LogFile)
	}

	executable, err := os.Executable()
	if err != nil {
		return spawned{}, die(ExitFailure, "locating the caveman binary failed")
	}
	command := exec.Command(executable)
	command.Dir = c.paths.Home
	command.Stdout = log
	command.Stderr = log
	command.Env = append(c.environ(), serveEnvVar+"=1", "PORT="+strconv.Itoa(port))
	// Its own session: a Ctrl-C in the launching terminal must not reach the
	// daemon, whose only shutdown path is the SIGTERM `down` sends. The daemon
	// still outlives this process — nothing here kills it on the way out.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return spawned{}, die(ExitFailure, "starting the server failed")
	}
	pid := command.Process.Pid
	if err := os.WriteFile(c.paths.PIDFile, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return spawned{}, die(ExitFailure, "writing %s failed", c.paths.PIDFile)
	}

	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()
	return spawned{pid: pid, exited: exited}, nil
}

func (c *CLI) startServer(port int, baseURL string, level string, count bool) *exitError {
	switch probeHealth(baseURL) {
	case HealthCaveman:
		c.streams.say("caveman already running on %s", baseURL)
		return nil
	case HealthForeign:
		return die(ExitPortTaken,
			"port %d is held by another process; stop it or set PORT", port)
	}

	c.clearStalePID()
	child, failure := c.spawnServer(port)
	if failure != nil {
		return failure
	}

	if failure := c.awaitReady(child, baseURL, port); failure != nil {
		os.Remove(c.paths.PIDFile)
		return failure
	}
	c.streams.say("caveman listening on %s", baseURL)
	c.streams.say("level: %s", level)
	c.streams.say("counting: %s", countLabel(count))
	c.streams.say("logs: %s", c.paths.LogFile)
	return nil
}

// echoSessionSummary lifts the summary out of the log, where stdout was
// redirected at launch, so it is not left where nobody looks.
func (c *CLI) echoSessionSummary() {
	file, err := os.Open(c.paths.LogFile)
	if err != nil {
		return
	}
	defer file.Close()
	tail := make([]string, 0, logTailLines)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		if len(tail) == logTailLines {
			tail = tail[1:]
		}
		tail = append(tail, scanner.Text())
	}
	for index := len(tail) - 1; index >= 0; index-- {
		if strings.HasPrefix(tail[index], summaryPrefix) {
			c.streams.say("%s", tail[index])
			return
		}
	}
}

func (c *CLI) awaitExit(pid int) bool {
	deadline := c.now().Add(termTimeout)
	for c.now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(stopInterval)
	}
	return false
}

// stopServer sends SIGTERM, never SIGKILL first: the server prints the session
// summary on SIGTERM and a hard kill skips it.
func (c *CLI) stopServer() *exitError {
	pid, alive := c.readPID()
	if !alive {
		os.Remove(c.paths.PIDFile)
		c.streams.say("caveman is not running")
		return nil
	}

	signalProcess(pid, syscall.SIGTERM)
	if c.awaitExit(pid) {
		c.streams.say("caveman stopped")
		c.echoSessionSummary()
	} else {
		signalProcess(pid, syscall.SIGKILL)
		c.streams.warn("caveman: pid %d ignored SIGTERM for %s; killed (session summary lost)",
			pid, formatSeconds(termTimeout))
	}
	os.Remove(c.paths.PIDFile)
	return nil
}

func signalProcess(pid int, signal syscall.Signal) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Signal(signal)
}

func (c *CLI) reportStatus(port int, baseURL string, level string, count bool) *exitError {
	if isRunning(baseURL) {
		c.streams.say("caveman is running on %s", baseURL)
		c.streams.say("level: %s", level)
		c.streams.say("counting: %s", countLabel(count))
		if pid, alive := c.readPID(); alive {
			c.streams.say("pid: %d", pid)
		}
		c.streams.say("logs: %s", c.paths.LogFile)
		return nil
	}
	c.streams.say("caveman is not running (port %d)", port)
	return quiet(ExitFailure)
}
