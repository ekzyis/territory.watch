// Command tw builds territory.watch: a static site showing the revenue a Stacker
// News territory earns its founder, rendered from real SN data. All the command
// logic lives in the cmd package; main just wires it to the process.
package main

import (
	"os"

	"github.com/ekzyis/territory.watch/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
