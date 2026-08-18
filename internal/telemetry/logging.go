package telemetry

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const disableVariable = "DISABLE_LOGS"

var falseValues = map[string]bool{"0": true, "false": true}

// Sink receives one finished log line, without its newline.
type Sink func(line string)

func SilentSink(string) {}

// LoggingEnabled is on unless DISABLE_LOGS says otherwise. Read per call rather
// than once at start so setting the variable takes effect immediately.
func LoggingEnabled() bool {
	configured, present := os.LookupEnv(disableVariable)
	if !present {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(configured))
	if normalized == "" {
		return true
	}
	return falseValues[normalized]
}

func NewSink() Sink { return NewSinkTo(os.Stdout) }

func NewSinkTo(writer io.Writer) Sink {
	return func(line string) {
		if !LoggingEnabled() {
			return
		}
		fmt.Fprintf(writer, "%s\n", line)
	}
}
