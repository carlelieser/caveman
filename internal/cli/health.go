package cli

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/carlelieser/caveman/internal/server"
)

const (
	readyTimeout  = 15 * time.Second
	probeInterval = 200 * time.Millisecond
	probeTimeout  = 2 * time.Second
	logTailLines  = 20
)

// serviceMarker is what separates our server from an unrelated one holding the
// port: a 200 alone proves only that the port is taken.
const serviceMarker = `"service":"caveman"`

// Health is the three states the probe distinguishes.
type Health string

const (
	HealthCaveman Health = "caveman"
	HealthForeign Health = "foreign"
	HealthDown    Health = "down"
)

// probeHealth reports which of the three states the port is in. Anything that
// answers over HTTP without the marker is foreign, whatever its status code.
func probeHealth(baseURL string) Health {
	client := &http.Client{Timeout: probeTimeout}
	response, err := client.Get(baseURL + server.HealthPath)
	if err != nil {
		return HealthDown
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return HealthForeign
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return HealthForeign
	}
	if strings.Contains(string(body), serviceMarker) {
		return HealthCaveman
	}
	return HealthForeign
}

func isRunning(baseURL string) bool { return probeHealth(baseURL) == HealthCaveman }

func exited(child spawned) bool {
	select {
	case <-child.exited:
		return true
	default:
		return false
	}
}

func (c *CLI) printLogTail() {
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
	c.streams.warn("--- last %d lines of %s ---", logTailLines, c.paths.LogFile)
	for _, line := range tail {
		c.streams.warn("%s", line)
	}
}

// awaitReady waits for the launched process to answer. Its exit is checked
// before probing, so a server that dies on boot reports the crash instead of
// burning the full budget.
func (c *CLI) awaitReady(child spawned, baseURL string, port int) *exitError {
	deadline := c.now().Add(readyTimeout)
	for c.now().Before(deadline) {
		if exited(child) {
			c.streams.warn("caveman: the server exited during startup")
			c.printLogTail()
			return quiet(ExitNotReady)
		}
		switch probeHealth(baseURL) {
		case HealthCaveman:
			return nil
		case HealthForeign:
			return quiet(ExitPortTaken)
		}
		time.Sleep(probeInterval)
	}
	c.streams.warn("caveman: the server did not answer on port %d within %s",
		port, formatSeconds(readyTimeout))
	c.printLogTail()
	return quiet(ExitNotReady)
}
