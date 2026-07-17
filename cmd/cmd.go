// Package cmd implements the tw subcommands that build territory.watch: a static
// site showing the revenue a Stacker News territory earns its founder, rendered
// from real SN data. The commands are two stdout-oriented primitives that pipe
// together; run tw with no arguments for the usage text.
package cmd

import (
	"fmt"
	"os"
)

// Run dispatches a tw invocation (os.Args[1:]) to a subcommand and returns the
// process exit code. Keeping this in the cmd package leaves main.go a one-liner.
func Run(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}

	var err error
	switch args[0] {
	case "fetch":
		err = fetchCmd(args[1:])
	case "aggregate":
		err = aggregateCmd(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "tw: unknown command %q\n\n", args[0])
		usage()
		return 2
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tw: %v\n", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `tw — territory.watch data fetcher + static site renderer

usage:
  tw fetch <territory>            fetch a territory's items as an NDJSON feed (stdout)
  tw aggregate                    aggregate the feed (stdin) into dashboard JSON (stdout)

example:
  tw fetch security | tw aggregate > security-agg.json
`)
}
