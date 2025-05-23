package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/igolaizola/musikai/pkg/cli"
)

// Build flags
var version = ""
var commit = ""
var date = ""

func main() {
	// Create signal based context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Launch command
	cmd := cli.New(version, commit, date)
	if err := cmd.ParseAndRun(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
