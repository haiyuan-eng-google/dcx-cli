// Package main is the entry point for the dcx CLI.
package main

import (
	"os"

	"github.com/haiyuan-eng-google/dcx-cli/internal/cli"
	dcxerrors "github.com/haiyuan-eng-google/dcx-cli/internal/errors"
)

func main() {
	app := cli.NewApp()

	if err := app.Root.Execute(); err != nil {
		dcxerrors.Emit(dcxerrors.Internal, err.Error(), "")
		os.Exit(dcxerrors.ExitInfra)
	}
}
