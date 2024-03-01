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
	"github.com/igolaizola/musikai/pkg/cmd/filter"
	"github.com/igolaizola/musikai/pkg/cmd/generate"
	"github.com/igolaizola/musikai/pkg/cmd/migrate"
	"github.com/peterbourgon/ff/ffyaml"
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
			newGenerateCommand(),
			newMigrateCommand(),
			newFilterCommand(),
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
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("musikai"),
		},
		ShortHelp: fmt.Sprintf("musikai %s command", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return musikai.GenerateSong(ctx, cfg, prompt, style, title, instrumental, output)
		},
	}
}

func newMigrateCommand() *ffcli.Command {
	cmd := "migrate"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &migrate.Config{}

	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")

	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("musikai %s [flags] <key> <value data...>", cmd),
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("MUSIKAI"),
		},
		ShortHelp: fmt.Sprintf("musikai %s action", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return migrate.Run(ctx, cfg)
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
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("musikai"),
		},
		ShortHelp: fmt.Sprintf("musikai %s command", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return analyze.Run(ctx, cfg)
		},
	}
}

func newGenerateCommand() *ffcli.Command {
	cmd := "generate"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &generate.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "timeout for the process (0 means no timeout)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "number of concurrent processes")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number iterations (0 means no limit)")
	fs.DurationVar(&cfg.WaitMin, "wait-min", 3*time.Second, "minimum wait time between songs")
	fs.DurationVar(&cfg.WaitMax, "wait-max", 1*time.Minute, "maximum wait time between songs")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy to use")
	fs.StringVar(&cfg.Output, "output", "", "output folder to download the songs (optional)")

	fs.StringVar(&cfg.Account, "account", "", "account to use")
	fs.StringVar(&cfg.Prompt, "prompt", "", "prompt to use")
	fs.StringVar(&cfg.Style, "style", "", "style to use")
	fs.BoolVar(&cfg.Instrumental, "instrumental", true, "instrumental song")
	fs.StringVar(&cfg.Type, "type", "", "type to use")

	fs.StringVar(&cfg.S3Bucket, "s3-bucket", "", "s3 bucket")
	fs.StringVar(&cfg.S3Region, "s3-region", "", "s3 region")
	fs.StringVar(&cfg.S3Key, "s3-key", "", "s3 key")
	fs.StringVar(&cfg.S3Secret, "s3-secret", "", "s3 secret")

	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("musikai %s [flags] <key> <value data...>", cmd),
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("MUSIKAI"),
		},
		ShortHelp: fmt.Sprintf("musikai %s action", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return generate.Run(ctx, cfg)
		},
	}
}

func newFilterCommand() *ffcli.Command {
	cmd := "filter"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &filter.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.IntVar(&cfg.Port, "port", 1337, "port to listen on")

	return &ffcli.Command{
		Name:       cmd,
		ShortUsage: fmt.Sprintf("musikai %s [flags] <key> <value data...>", cmd),
		Options: []ff.Option{
			ff.WithConfigFileFlag("config"),
			ff.WithConfigFileParser(ffyaml.Parser),
			ff.WithEnvVarPrefix("MUSIKAI"),
		},
		ShortHelp: fmt.Sprintf("musikai %s action", cmd),
		FlagSet:   fs,
		Exec: func(ctx context.Context, args []string) error {
			return filter.Serve(ctx, cfg)
		},
	}
}
