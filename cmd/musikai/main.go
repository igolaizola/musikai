package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/cmd/analyze"
	"github.com/igolaizola/musikai/pkg/cmd/cover"
	"github.com/igolaizola/musikai/pkg/cmd/download"
	"github.com/igolaizola/musikai/pkg/cmd/draft"
	"github.com/igolaizola/musikai/pkg/cmd/filter"
	"github.com/igolaizola/musikai/pkg/cmd/generate"
	"github.com/igolaizola/musikai/pkg/cmd/migrate"
	"github.com/igolaizola/musikai/pkg/cmd/process"
	"github.com/igolaizola/musikai/pkg/cmd/title"
	"github.com/igolaizola/musikai/pkg/cmd/upscale"
	"github.com/igolaizola/musikai/pkg/imageai"
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
			newMigrateCommand(),
			newAnalyzeCommand(),
			newGenerateCommand(),
			newProcessCommand(),
			newTitleCommand(),
			newDraftCommand(),
			newCoverCommand(),
			newUpscaleCommand(),
			newFilterCommand(),
			newDownloadCommand(),
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

	fs.StringVar(&cfg.Account, "account", "", "account to use")
	fs.StringVar(&cfg.Prompt, "prompt", "", "prompt to use")
	fs.StringVar(&cfg.Style, "style", "", "style to use")
	fs.BoolVar(&cfg.Instrumental, "instrumental", true, "instrumental song")
	fs.StringVar(&cfg.Type, "type", "", "type to use")
	fs.StringVar(&cfg.EndLyrics, "end-lyrics", "[end]", "end lyrics text to use")
	fs.StringVar(&cfg.EndStyle, "end-style", ". End", "end style to use, leave empty to use the style of the song")
	fs.BoolVar(&cfg.EndStyleAppend, "end-style-append", true, "append end style instead of replacing it")
	fs.StringVar(&cfg.ForceEndLyrics, "force-end-lyrics", "[end]", "force end lyrics text to use")
	fs.StringVar(&cfg.ForceEndStyle, "force-end-style", "short, end", "force end style to use")
	fs.DurationVar(&cfg.MinDuration, "min-duration", 0, "minimum duration for the song")
	fs.DurationVar(&cfg.MaxDuration, "max-duration", 0, "maximum duration for the song")
	fs.IntVar(&cfg.MaxExtensions, "max-extensions", 0, "maximum number of extensions for the song")
	fs.StringVar(&cfg.Notes, "notes", "", "text notes stored with the song")

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

func newProcessCommand() *ffcli.Command {
	cmd := "process"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &process.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "timeout for the process (0 means no timeout)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "number of concurrent processes")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number iterations (0 means no limit)")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy to use")

	fs.StringVar(&cfg.Type, "type", "", "type to use")
	fs.BoolVar(&cfg.Reprocess, "reprocess", false, "reprocess the song")
	fs.DurationVar(&cfg.ShortFadeOut, "short-fadeout", 0, "short fade out duration")
	fs.DurationVar(&cfg.LongFadeOut, "long-fadeout", 0, "long fade out duration")

	fs.StringVar(&cfg.S3Bucket, "s3-bucket", "", "s3 bucket")
	fs.StringVar(&cfg.S3Region, "s3-region", "", "s3 region")
	fs.StringVar(&cfg.S3Key, "s3-key", "", "s3 key")
	fs.StringVar(&cfg.S3Secret, "s3-secret", "", "s3 secret")

	fs.StringVar(&cfg.TGToken, "tg-token", "", "telegram token")
	fs.Int64Var(&cfg.TGChat, "tg-chat", 0, "telegram chat")

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
			return process.Run(ctx, cfg)
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
	fsMapVar(fs, &cfg.Credentials, "creds", nil, "credentials to use")

	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy to use")
	fs.StringVar(&cfg.TGToken, "tg-token", "", "telegram token")
	fs.Int64Var(&cfg.TGChat, "tg-chat", 0, "telegram chat")

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

func newUpscaleCommand() *ffcli.Command {
	cmd := "upscale"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &upscale.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "timeout for the process (0 means no timeout)")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number of images to process (0 means no limit)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "number of concurrent processes")
	fs.StringVar(&cfg.Type, "type", "", "filter by type")

	// Upscale parameters
	fs.StringVar(&cfg.UpscaleType, "upscale-type", "topaz", "upscale type (topaz, esrgan)")
	fs.StringVar(&cfg.UpscaleBin, "upscale-bin", "", "upscale binary path")

	// Telegram parameters
	fs.StringVar(&cfg.TelegramToken, "tg-token", "", "telegram token")
	fs.Int64Var(&cfg.TelegramChat, "tg-chat", 0, "telegram chat id")
	fs.StringVar(&cfg.TelegramProxy, "tg-proxy", "", "telegram proxy")
	fs.IntVar(&cfg.TelegramConcurrency, "tg-concurrency", 1, "number of concurrent uploads")

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
			return upscale.Run(ctx, cfg)
		},
	}
}

func newTitleCommand() *ffcli.Command {
	cmd := "title"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &title.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number iterations (0 means no limit)")
	fs.StringVar(&cfg.Input, "input", "", "input file")
	fs.StringVar(&cfg.Type, "type", "", "default type to use (can be override by the input file)")

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
			return title.Run(ctx, cfg)
		},
	}
}

func newDraftCommand() *ffcli.Command {
	cmd := "draft"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &draft.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number iterations (0 means no limit)")
	fs.StringVar(&cfg.Input, "input", "", "input file")
	fs.StringVar(&cfg.Type, "type", "", "default type to use (can be override by the input file)")
	fs.IntVar(&cfg.Volumes, "volumes", 0, "default volumes to use (can be override by the input file)")

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
			return draft.Run(ctx, cfg)
		},
	}
}

func newCoverCommand() *ffcli.Command {
	cmd := "cover"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &cover.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.Type, "type", "", "type to use")
	fs.StringVar(&cfg.Template, "template", "", "template to use")
	fs.IntVar(&cfg.Minimum, "minimum", 0, "minimum number of covers to generate per album")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "timeout for the process (0 means no timeout)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "number of concurrent processes")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number of images to process (0 means no limit)")
	fs.DurationVar(&cfg.WaitMin, "wait-min", 3*time.Second, "minimum wait time between images")
	fs.DurationVar(&cfg.WaitMax, "wait-max", 1*time.Minute, "maximum wait time between images")

	// Discord parameters
	cfg.Discord = &imageai.Config{}
	fs.StringVar(&cfg.Discord.Bot, "bot", "midjourney", "discord bot")
	fs.StringVar(&cfg.Discord.Proxy, "proxy", "", "discord proxy")
	fs.StringVar(&cfg.Discord.Channel, "channel", "", "discord channel id")
	fs.StringVar(&cfg.Discord.ReplicateToken, "replicate-token", "", "replicate token")

	// Session
	fs.StringVar(&cfg.Discord.SessionFile, "session", "session.yaml", "session config file (optional)")

	fsSession := flag.NewFlagSet("", flag.ExitOnError)
	for _, fs := range []*flag.FlagSet{fs, fsSession} {
		fs.StringVar(&cfg.Discord.Session.UserAgent, "user-agent", "", "user agent")
		fs.StringVar(&cfg.Discord.Session.JA3, "ja3", "", "ja3 fingerprint")
		fs.StringVar(&cfg.Discord.Session.Language, "language", "", "language")
		fs.StringVar(&cfg.Discord.Session.Token, "token", "", "authentication token")
		fs.StringVar(&cfg.Discord.Session.SuperProperties, "super-properties", "", "super properties")
		fs.StringVar(&cfg.Discord.Session.Locale, "locale", "", "locale")
		fs.StringVar(&cfg.Discord.Session.Cookie, "cookie", "", "cookie")
	}

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
			if err := loadSession(fsSession, cfg.Discord.SessionFile); err != nil {
				return fmt.Errorf("couldn't load session: %w", err)
			}
			cfg.Discord.Debug = cfg.Debug
			return cover.Run(ctx, cfg)
		},
	}
}

func loadSession(fs *flag.FlagSet, file string) error {
	if file == "" {
		return fmt.Errorf("session file not specified")
	}
	if _, err := os.Stat(file); err != nil {
		return nil
	}
	log.Printf("loading session from %s", file)
	return ff.Parse(fs, []string{}, []ff.Option{
		ff.WithConfigFile(file),
		ff.WithConfigFileParser(ffyaml.Parser),
	}...)
}

func newDownloadCommand() *ffcli.Command {
	cmd := "download"
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	_ = fs.String("config", "", "config file (optional)")

	cfg := &download.Config{}

	fs.BoolVar(&cfg.Debug, "debug", false, "debug mode")
	fs.StringVar(&cfg.DBType, "db-type", "", "db type (local, sqlite, mysql, postgres)")
	fs.StringVar(&cfg.DBConn, "db-conn", "", "path for sqlite, dsn for mysql or postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", 0, "timeout for the process (0 means no timeout)")
	fs.IntVar(&cfg.Concurrency, "concurrency", 1, "number of concurrent processes")
	fs.IntVar(&cfg.Limit, "limit", 0, "limit the number iterations (0 means no limit)")
	fs.StringVar(&cfg.Proxy, "proxy", "", "proxy to use")
	fs.StringVar(&cfg.TGToken, "tg-token", "", "telegram token")
	fs.Int64Var(&cfg.TGChat, "tg-chat", 0, "telegram chat")
	fs.StringVar(&cfg.Type, "type", "", "type to use")
	fs.StringVar(&cfg.Output, "output", ".cache", "output folder")

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
			return download.Run(ctx, cfg)
		},
	}
}

type mapValue struct {
	v *map[string]string
}

func (m *mapValue) String() string {
	if m.v == nil {
		return ""
	}
	return fmt.Sprintf("%v", map[string]string(*m.v))
}

func (m *mapValue) Set(value string) error {
	if m.v == nil {
		return errors.New("nil map reference")
	}
	pairs := strings.Split(value, ";")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid map entry: %s", pair)
		}
		(*m.v)[parts[0]] = parts[1]
	}
	return nil
}

func fsMapVar(fs *flag.FlagSet, p *map[string]string, name string, value map[string]string, usage string) {
	if value == nil {
		value = make(map[string]string)
	}
	*p = value
	fs.Var(&mapValue{p}, name, usage)
}
