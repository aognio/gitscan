// Command gitscan discovers Git repositories under configured roots and
// collects per-repo stats concurrently. See the README and
// .development/ (engineering memory) for design rationale.
package main

import (
	"fmt"
	"os"

	"github.com/aognio/gitscan/cmd"
)

func main() {
	if err := cmd.New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gitscan:", err)
		os.Exit(1)
	}
}