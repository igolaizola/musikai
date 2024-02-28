package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/igolaizola/musikai"
	"github.com/igolaizola/musikai/pkg/cmd/analyze"
	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
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
	cmd := newCommand()
	if err := cmd.ParseAndRun(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func newCommand() *ffcli.Command {
	fs := flag.NewFlagSet("musikai", flag.ExitOnError)

	return &ffcli.Command{
		ShortUsage: "musikai [flags] <subcommand>",
		FlagSet:    fs,
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ffcli.Command{
			newVersionCommand(),
			newSongCommand(),
			newAnalyzeCommand(),
		},
	}
}

func newVersionCommand() *ffcli.Command {
	return &ffcli.Command{
		Name:       "version",
		ShortUsage: "musikai version",
		ShortHelp:  "print version",
		Exec: func(ctx context.Context, args []string) error {
			v := version
			if v == "" {
				if buildInfo, ok := debug.ReadBuildInfo(); ok {
					v = buildInfo.Main.Version
				}
			}
			if v == "" {
				v = "dev"
			}
			versionFields := []string{v}
			if commit != "" {
				versionFields = append(versionFields, commit)
			}
			if date != "" {
				versionFields = append(versionFields, date)
			}
			fmt.Println(strings.Join(versionFields, " "))
			return nil
		},
	}
}

func newSongCommand() *ffcli.Command {
	cmd := "song"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &musikai.Config{}
	fs.StringVar(&cfg.Cookie, "cookie", "", "cookie file")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy")
	fs.DurationVar(&cfg.Wait, "wait", 4*time.Second, "wait time")
	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")

	var prompt, style, title string
	var instrumental bool
	fs.StringVar(&prompt, "prompt", "", "prompt to autogenerate the song")
	fs.StringVar(&style, "style", "", "style of the song")
	fs.StringVar(&title, "title", "", "title for the song")
	fs.BoolVar(&instrumental, "instrumental", false, "instrumental song")
	var output string
	fs.StringVar(&output, "output", "", "output file or folder")

	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("musikai %s [flags] <key> <value data...>", cmd),
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ff.PlainParser),
			ff.WithEnvVarPrefix("musikai"),
		},
		ShortHelp: fmt.Sprintf("musikai %s command", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return musikai.GenerateSong(ctx, cfg, prompt, style, title, instrumental, output)
		},
	}
}

func newAnalyzeCommand() *ffcli.Command {
	cmd := "analyze"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &analyze.Config{}
	fs.StringVar(&cfg.Input, "input", "", "input file")
	fs.StringVar(&cfg.Output, "output", "", "output folder")

	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("musikai %s [flags] <key> <value data...>", cmd),
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ff.PlainParser),
			ff.WithEnvVarPrefix("musikai"),
		},
		ShortHelp: fmt.Sprintf("musikai %s command", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return analyze.Run(ctx, cfg)
		},
	}
}
