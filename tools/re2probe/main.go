// Compiles every pattern the generator is about to emit, using Go's own regexp
// parser. Run from tools/gen-go.mjs so an init-time panic can never ship.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

type pattern struct {
	Pattern string `json:"pattern"`
	Where   string `json:"where"`
}

func main() {
	var pats []pattern
	if err := json.NewDecoder(os.Stdin).Decode(&pats); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	bad := 0
	for _, p := range pats {
		if _, err := regexp.Compile(p.Pattern); err != nil {
			fmt.Printf("%s\n  pattern: %s\n  error:   %v\n", p.Where, p.Pattern, err)
			bad++
		}
	}
	if bad > 0 {
		fmt.Printf("%d of %d patterns do not compile\n", bad, len(pats))
		os.Exit(1)
	}
}
