package main

import (
	"os"

	"github.com/carlelieser/caveman/internal/cli"
)

func main() {
	streams := cli.Streams{Out: os.Stdout, Err: os.Stderr}
	if cli.ServeRequested(os.LookupEnv) {
		os.Exit(cli.Serve(streams))
	}
	executable, err := os.Executable()
	if err != nil {
		executable = os.Args[0]
	}
	command := cli.New(cli.Config{
		Streams:    streams,
		Environ:    os.Environ(),
		Lookup:     os.LookupEnv,
		Executable: executable,
	})
	os.Exit(command.Run(os.Args[1:]))
}
