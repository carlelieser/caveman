package cli

import (
	"os"
	"strings"

	"github.com/carlelieser/caveman/internal/policy"
)

// LevelOff is the CLI's spelling of "send no header". The policy parser accepts
// it on the wire, but a client launched at this level sends nothing at all.
const LevelOff = "off"

// Asking for the CLI is asking for compression: `caveman claude` that forwards
// uncompressed is the proxy doing nothing. The server defaults to `off` for the
// opposite reason — it must forward byte-identical to a client that never asked.
const DefaultLevel = string(policy.LevelCaveman)

func levelNames() []string {
	names := make([]string, 0, len(policy.LevelNames)+1)
	names = append(names, LevelOff)
	for _, level := range policy.LevelNames {
		names = append(names, string(level))
	}
	return names
}

// LevelList is the flag's help text, in the same order the bash CLI printed.
func LevelList() string { return strings.Join(levelNames(), " ") }

func isLevel(value string) bool {
	return value == LevelOff || policy.IsLevel(value)
}

// parsed is one command line with the level flag taken out.
type parsed struct {
	// level is empty when no flag was given, which is what lets a stored level
	// show through.
	level string
	// count is nil when neither --count nor --no-count was given, which is
	// what lets a stored setting show through. A pointer rather than a bool
	// because "not said" and "said no" resolve differently.
	count *bool
	args  []string
}

// parseFlags strips Caveman's own flags and leaves the rest, so the client
// only what belongs to it.
func parseFlags(argv []string) (parsed, *exitError) {
	result := parsed{args: []string{}}
	for index := 0; index < len(argv); index++ {
		argument := argv[index]
		switch {
		case argument == "--":
			// Everything after `--` belongs to the client, including its own -l.
			result.args = append(result.args, argv[index+1:]...)
			return result, nil
		case argument == countFlag:
			yes := true
			result.count = &yes
		case argument == noCountFlag:
			no := false
			result.count = &no
		case argument == "--level" || argument == "-l":
			if index+1 >= len(argv) {
				return result, die(ExitUsage, "reading %s failed: no level given", argument)
			}
			value := argv[index+1]
			if !isLevel(value) {
				return result, requireLevel(value)
			}
			result.level = value
			index++
		case strings.HasPrefix(argument, "--level="):
			value := strings.TrimPrefix(argument, "--level=")
			if !isLevel(value) {
				return result, requireLevel(value)
			}
			result.level = value
		default:
			result.args = append(result.args, argument)
		}
	}
	return result, nil
}

func requireLevel(value string) *exitError {
	return die(ExitUsage, "reading --level failed: %q is not one of %s", value, LevelList())
}

func (p Paths) readStoredLevel() (string, bool) {
	contents, err := os.ReadFile(p.LevelFile)
	if err != nil {
		return "", false
	}
	stored := string(contents)
	if !isLevel(stored) {
		return "", false
	}
	return stored, true
}

func (p Paths) storeLevel(level string) *exitError {
	if failure := p.ensureRunDir(); failure != nil {
		return failure
	}
	if err := os.WriteFile(p.LevelFile, []byte(level), 0o644); err != nil {
		return die(ExitFailure, "writing the level to %s failed", p.LevelFile)
	}
	return nil
}

// resolveLevel: a level on this command wins; otherwise the one `up` stored;
// otherwise the CLI default.
func (p Paths) resolveLevel(flag string) string {
	if flag != "" {
		return flag
	}
	if stored, ok := p.readStoredLevel(); ok {
		return stored
	}
	return DefaultLevel
}
