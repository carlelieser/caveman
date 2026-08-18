package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/carlelieser/caveman/internal/policy"
)

// A client is launched, never returned from: it takes over the process so it
// owns the TTY, receives signals directly, and its exit code becomes ours.
type client func(c *CLI, launch launchContext) *exitError

// launchContext is what every client is told about the proxy it should use.
type launchContext struct {
	BaseURL string
	Level   string
	Args    []string
}

// builtins are compiled in, so a single binary with nothing beside it on disk
// still launches its clients. A file in the client directory shadows a builtin
// of the same name.
var builtins = map[string]client{"claude": launchClaude}

// compressHeader is the header value a launch sends, or empty at `off`. `off`
// sends no header at all, so the request forwards byte-identical rather than
// relying on the server to parse a level that means "do nothing".
func compressHeader(level string) string {
	if level == LevelOff {
		return ""
	}
	return policy.CompressHeader + ": " + level
}

func launchClaude(c *CLI, launch launchContext) *exitError {
	path, err := exec.LookPath("claude")
	if err != nil {
		return die(ExitNoClient, "launching claude failed: not found on PATH")
	}
	environment := append(c.environ(),
		"ANTHROPIC_BASE_URL="+launch.BaseURL,
		"ANTHROPIC_CUSTOM_HEADERS="+compressHeader(launch.Level),
		"ENABLE_TOOL_SEARCH=true",
	)
	argv := append([]string{"claude"}, launch.Args...)
	return c.exec(path, argv, environment)
}

// clientScript is the drop-in contract: an executable file named for the client,
// run with the proxy's settings in its environment. The bash CLI sourced a
// `client_launch` shell function, which a single Go binary cannot do; an exec
// keeps the property that mattered — a client is a file dropped into a
// directory, with no registry to edit.
func (c *CLI) clientScript(name string) (string, bool) {
	path := filepath.Join(c.paths.ClientDir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func (c *CLI) hasClient(name string) bool {
	if _, ok := builtins[name]; ok {
		return true
	}
	_, ok := c.clientScript(name)
	return ok
}

// clientNames merges the builtins with the directory listing, so help can never
// drift from what is actually installed.
func (c *CLI) clientNames() []string {
	seen := map[string]bool{}
	for name := range builtins {
		seen[name] = true
	}
	entries, err := os.ReadDir(c.paths.ClientDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			seen[entry.Name()] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *CLI) launchScript(path string, launch launchContext) *exitError {
	environment := append(c.environ(),
		"CAVEMAN_BASE_URL="+launch.BaseURL,
		"CAVEMAN_LEVEL="+launch.Level,
		"CAVEMAN_COMPRESS_HEADER="+compressHeader(launch.Level),
	)
	argv := append([]string{path}, launch.Args...)
	return c.exec(path, argv, environment)
}

// launchClient starts the server if needed, then hands the process over. Quiet
// when the server is already up, since this runs before every client launch.
func (c *CLI) launchClient(name string, port int, baseURL string, launch launchContext) *exitError {
	if !isRunning(baseURL) {
		if failure := c.startServer(port, baseURL, launch.Level); failure != nil {
			return failure
		}
	}
	if path, ok := c.clientScript(name); ok {
		return c.launchScript(path, launch)
	}
	return builtins[name](c, launch)
}

// execProcess replaces this process with the client. Overridden in tests, which
// cannot survive an execve.
func execProcess(path string, argv []string, environment []string) error {
	return syscall.Exec(path, argv, environment)
}
