package cli_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/carlelieser/caveman/internal/cli"
)

// runMeasure drives the command from the repository root, which is where the
// recorded corpus lives.
func runMeasure(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	command := cli.New(cli.Config{
		Streams: cli.Streams{Out: out, Err: errs},
		Lookup:  func(string) (string, bool) { return "", false },
	})
	t.Chdir("../..")
	code := command.Run(append([]string{"measure"}, args...))
	return out.String(), errs.String(), code
}

// The compressed totals aggregate every stage — region protection, tagging,
// level selection and assembly — so a change anywhere in the pipeline moves at
// least one of them.
//
// Only the output totals are pinned. The input total is a `ceil(chars/4)`
// estimate over character counts, and one fixture carries an em dash, which is
// one UTF-16 unit but three UTF-8 bytes; the resulting one-token difference
// puts the caveman percentage within rounding distance of a boundary. Pinning
// either would assert a counting convention rather than what compression did.
func TestMeasureReproducesTheCompressedTotals(t *testing.T) {
	stdout, stderr, code := runMeasure(t)
	if code != 0 {
		t.Fatalf("measure exited %d: %s", code, stderr)
	}
	totals := map[string]string{"light": "5,880 tok", "moderate": "4,935 tok", "caveman": "4,442 tok"}
	for level, want := range totals {
		line := lineNaming(stdout, level)
		if line == "" {
			t.Errorf("no corpus line for %s\n%s", level, stdout)
			continue
		}
		if !strings.Contains(line, want) {
			t.Errorf("%s line = %q, want it to report %q", level, line, want)
		}
	}
}

// lineNaming returns the first corpus line whose first field is the level.
func lineNaming(stdout, level string) string {
	for line := range strings.SplitSeq(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == level {
			return line
		}
	}
	return ""
}

// The per-request rows are what show that savings track the prose share rather
// than the request size.
func TestMeasureReportsEveryRequestSortedByProseShare(t *testing.T) {
	stdout, _, code := runMeasure(t)
	if code != 0 {
		t.Fatalf("measure exited %d", code)
	}
	names := []string{
		"beginner rambling about a react state bug",
		"formal bug report with stack trace and package m",
		"terse expert asking about a postgres plan",
		"dense prose with no code at all",
		"code review request that is mostly a pasted diff",
	}
	for _, name := range names {
		if !strings.Contains(stdout, name) {
			t.Errorf("output does not report %q\n%s", name, stdout)
		}
	}

	shares := proseShares(stdout)
	if len(shares) != 13 {
		t.Fatalf("reported %d rows, want 13", len(shares))
	}
	for index := 1; index < len(shares); index++ {
		if shares[index] > shares[index-1] {
			t.Errorf("row %d has a %d%% prose share, above the %d%% before it",
				index, shares[index], shares[index-1])
		}
	}
}

// proseShares reads the percentage out of each per-request row, in the order
// they were printed.
func proseShares(stdout string) []int {
	shares := []int{}
	for line := range strings.SplitSeq(stdout, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field != "prose" || index == 0 {
				continue
			}
			share, err := strconv.Atoi(strings.TrimSuffix(fields[index-1], "%"))
			if err == nil {
				shares = append(shares, share)
			}
		}
	}
	return shares
}

func TestMeasureRejectsAnUnknownFlag(t *testing.T) {
	_, stderr, code := runMeasure(t, "--nonsense")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "--nonsense") {
		t.Errorf("stderr = %q", stderr)
	}
}

// The performance mode times the same corpus. What it reports varies by
// machine, so only its shape is pinned.
func TestMeasurePerformanceReportsEveryLevel(t *testing.T) {
	stdout, stderr, code := runMeasure(t, "--performance")
	if code != 0 {
		t.Fatalf("measure --performance exited %d: %s", code, stderr)
	}
	for _, want := range []string{"light", "moderate", "caveman", "prose chars/s", "ms"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not carry %q\n%s", want, stdout)
		}
	}
}
