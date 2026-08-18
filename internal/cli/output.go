package cli

import (
	"fmt"
	"io"
)

const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitPortTaken = 3
	ExitNotReady  = 4
	ExitNoClient  = 127
)

// Streams is where one invocation reads its environment and writes its output.
// Held rather than reaching for os.Stdout directly so a test can run the whole
// dispatch in-process.
type Streams struct {
	Out io.Writer
	Err io.Writer
}

func (s Streams) say(format string, args ...any) {
	fmt.Fprintf(s.Out, format+"\n", args...)
}

func (s Streams) warn(format string, args ...any) {
	fmt.Fprintf(s.Err, format+"\n", args...)
}

// exitError carries the code the process should end with. Returned rather than
// calling os.Exit so the same paths run under `go test`.
type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string { return e.message }

// die names the operation that failed, matching the bash `caveman: ...` prefix.
func die(code int, format string, args ...any) *exitError {
	return &exitError{code: code, message: fmt.Sprintf(format, args...)}
}

// quiet exits with a code and nothing to print, for a command that already said
// what happened.
func quiet(code int) *exitError {
	return &exitError{code: code}
}
