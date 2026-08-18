package cli

import (
	"os"
	"path/filepath"
)

// Paths is the run directory and the files under it.
type Paths struct {
	Home      string
	RunDir    string
	PIDFile   string
	LogFile   string
	LevelFile string
	ClientDir string
	CountFile string
}

const (
	pidFileName   = "caveman.pid"
	logFileName   = "caveman.log"
	levelFileName = "caveman.level"
	countFileName = "caveman.count"
)

// resolveHome falls back to the directory holding the binary. The bash CLI
// resolved an install root because it had TypeScript sources to run from; the Go
// binary carries the server, so home matters only as the default parent of the
// run directory.
func resolveHome(lookup func(string) (string, bool), executable string) string {
	if home, ok := lookup("CAVEMAN_HOME"); ok && home != "" {
		return home
	}
	return filepath.Dir(executable)
}

func NewPaths(lookup func(string) (string, bool), executable string) Paths {
	home := resolveHome(lookup, executable)
	runDir, ok := lookup("CAVEMAN_RUN_DIR")
	if !ok || runDir == "" {
		runDir = filepath.Join(home, "run")
	}
	clientDir, ok := lookup("CAVEMAN_CLIENT_DIR")
	if !ok || clientDir == "" {
		clientDir = filepath.Join(home, "clients")
	}
	return Paths{
		Home:      home,
		RunDir:    runDir,
		PIDFile:   filepath.Join(runDir, pidFileName),
		LogFile:   filepath.Join(runDir, logFileName),
		LevelFile: filepath.Join(runDir, levelFileName),
		CountFile: filepath.Join(runDir, countFileName),
		ClientDir: clientDir,
	}
}

func (p Paths) ensureRunDir() *exitError {
	if err := os.MkdirAll(p.RunDir, 0o755); err != nil {
		return die(ExitFailure, "creating run directory %s failed", p.RunDir)
	}
	return nil
}
