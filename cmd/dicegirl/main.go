// Command dicegirl is the CLI front end for the dicegirl TRPG dice and rule
// engine. Interface code (argument parsing, text/JSON rendering) lives in
// cmd/dicegirl/cli and depends on the reusable core in internal/; nothing in
// internal/ knows about a user interface. This file only wires signals and
// process exit codes.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"dicegirl/cmd/dicegirl/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
