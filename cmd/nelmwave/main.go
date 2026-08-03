// Command nelmwave is a declarative release orchestrator on top of nelm.
package main

import (
	"os"

	"github.com/helmwave/nelmwave/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
