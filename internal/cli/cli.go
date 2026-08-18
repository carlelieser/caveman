package cli

import (
	"os"
	"strconv"
	"time"

	"github.com/carlelieser/caveman/internal/server"
)

// CLI is one invocation: where it writes, what it reads its environment from,
// and the paths that environment resolved to.
type CLI struct {
	streams Streams
	paths   Paths
	lookup  func(string) (string, bool)
	env     []string

	// now and exec are seams. Tests need a clock they can hold still and a
	// launch that returns instead of replacing the process.
	now  func() time.Time
	exec func(path string, argv []string, environment []string) *exitError
}

type Config struct {
	Streams    Streams
	Environ    []string
	Lookup     func(string) (string, bool)
	Executable string
}

func New(config Config) *CLI {
	lookup := config.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	cli := &CLI{
		streams: config.Streams,
		paths:   NewPaths(lookup, config.Executable),
		lookup:  lookup,
		env:     config.Environ,
		now:     time.Now,
	}
	cli.exec = func(path string, argv []string, environment []string) *exitError {
		if err := execProcess(path, argv, environment); err != nil {
			return die(ExitNoClient, "launching %s failed: %s", argv[0], err)
		}
		return nil
	}
	return cli
}

// Version is stamped at build time with -ldflags "-X ...cli.Version=<tag>". A
// binary built any other way reports "dev", which is how a release artifact is
// told apart from a local build when someone reports a bug.
var Version = "dev"

func (c *CLI) environ() []string { return c.env }

func (c *CLI) usage(out func(string, ...any)) {
	out("caveman — a compression proxy for LLM requests")
	out("")
	out("usage:")
	out("  caveman up [-l LEVEL]         start the proxy in the background")
	out("  caveman down                  stop the proxy")
	out("  caveman status                report whether the proxy is running")
	out("  caveman <client> [-l LEVEL]   start the proxy, then launch a client")
	out("  caveman measure [--performance]  report savings over the recorded corpus")
	out("  caveman version               report the build version")
	out("")
	out("  -l, --level LEVEL       %s (default %s)", LevelList(), DefaultLevel)
	out("                          a level given to up is inherited by clients")
	out("")
	out("clients:")
	names := c.clientNames()
	if len(names) == 0 {
		out("  (none installed)")
		return
	}
	for _, name := range names {
		out("  %s", name)
	}
}

// initPort asks the server's own ListenPort rather than parsing PORT here, so
// the CLI and the server can never disagree about precedence or validity.
func (c *CLI) initPort() (int, string, *exitError) {
	port, err := server.ListenPort()
	if err != nil {
		c.streams.warn("caveman: reading the configured port failed")
		c.streams.warn("  %s", err)
		return 0, "", quiet(ExitFailure)
	}
	return port, "http://localhost:" + strconv.Itoa(port), nil
}

// Run dispatches one command line and returns the process exit code.
func (c *CLI) Run(argv []string) int {
	failure := c.dispatch(argv)
	if failure == nil {
		return ExitOK
	}
	if failure.message != "" {
		c.streams.warn("caveman: %s", failure.message)
	}
	return failure.code
}

func (c *CLI) dispatch(argv []string) *exitError {
	command := ""
	if len(argv) > 0 {
		command = argv[0]
		argv = argv[1:]
	}

	// measure takes its own flags and never starts a server, so it is answered
	// before the level flag is read out of the line.
	if command == "measure" {
		return c.measure(argv)
	}

	parsedArgs, failure := parseFlags(argv)
	if failure != nil {
		return failure
	}

	switch command {
	case "up":
		port, baseURL, failure := c.initPort()
		if failure != nil {
			return failure
		}
		level := c.paths.resolveLevel(parsedArgs.level)
		if failure := c.paths.storeLevel(level); failure != nil {
			return failure
		}
		count := c.paths.resolveCount(parsedArgs.count)
		if failure := c.paths.storeCount(count); failure != nil {
			return failure
		}
		return c.startServer(port, baseURL, level, count)
	case "down":
		if _, _, failure := c.initPort(); failure != nil {
			return failure
		}
		return c.stopServer()
	case "status":
		port, baseURL, failure := c.initPort()
		if failure != nil {
			return failure
		}
		return c.reportStatus(port, baseURL, c.paths.resolveLevel(parsedArgs.level), c.paths.resolveCount(parsedArgs.count))
	case "version", "--version", "-v":
		c.streams.say("caveman %s", Version)
		return nil
	case "help", "--help", "-h":
		c.usage(c.streams.say)
		return nil
	case "":
		c.usage(c.streams.warn)
		return quiet(ExitUsage)
	default:
		if !c.hasClient(command) {
			c.streams.warn("caveman: unknown command %q", command)
			c.usage(c.streams.warn)
			return quiet(ExitUsage)
		}
		port, baseURL, failure := c.initPort()
		if failure != nil {
			return failure
		}
		return c.launchClient(command, port, baseURL, launchContext{
			BaseURL: baseURL,
			Level:   c.paths.resolveLevel(parsedArgs.level),
			Count:   c.paths.resolveCount(parsedArgs.count),
			Args:    parsedArgs.args,
		})
	}
}
