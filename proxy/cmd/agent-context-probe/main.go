package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/seifghazi/claude-code-monitor/internal/app"
	"github.com/seifghazi/claude-code-monitor/internal/buildinfo"
	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/service"
	"github.com/seifghazi/claude-code-monitor/internal/webui"
)

type commandOptions struct {
	configPath string
	dataDir    string
	host       string
	port       string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	command := "start"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "start":
		options, ok := parseOptions("start", args, stderr)
		if !ok {
			return 2
		}
		return start(options, stdout, stderr)
	case "doctor":
		options, ok := parseOptions("doctor", args, stderr)
		if !ok {
			return 2
		}
		return doctor(options, stdout, stderr)
	case "version":
		if len(args) != 0 {
			fmt.Fprintln(stderr, "version does not accept arguments")
			return 2
		}
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "agent-context-probe %s (commit %s, built %s)\n", info.Version, info.Commit, info.BuildTime)
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", command)
		printUsage(stderr)
		return 2
	}
}

func parseOptions(command string, args []string, stderr io.Writer) (commandOptions, bool) {
	var options commandOptions
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.configPath, "config", "", "path to config.yaml")
	flags.StringVar(&options.dataDir, "data-dir", "", "directory for requests.db")
	flags.StringVar(&options.host, "host", "", "listen host override")
	flags.StringVar(&options.port, "port", "", "listen port override")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "%s received unexpected arguments: %v\n", command, flags.Args())
		return commandOptions{}, false
	}
	return options, true
}

func resolveConfig(options commandOptions) (*config.Resolution, error) {
	return config.Resolve(config.LoadOptions{
		ConfigPath: options.configPath,
		DataDir:    options.dataDir,
		Host:       options.host,
		Port:       options.port,
	})
}

func start(options commandOptions, stdout, stderr io.Writer) int {
	resolution, err := resolveConfig(options)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 1
	}
	if err := resolution.Prepare(); err != nil {
		fmt.Fprintf(stderr, "configuration preparation error: %v\n", err)
		return 1
	}
	logger := log.New(stdout, "agent-context-probe: ", log.LstdFlags)
	application, err := app.New(resolution.Config, logger, app.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "startup error: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := application.Run(ctx); err != nil {
		fmt.Fprintf(stderr, "runtime error: %v\n", err)
		return 1
	}
	return 0
}

func doctor(options commandOptions, stdout, stderr io.Writer) int {
	resolution, err := resolveConfig(options)
	if err != nil {
		fmt.Fprintf(stderr, "[FAIL] configuration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "[PASS] configuration: %s (%s)\n", displayConfigPath(resolution), resolution.ConfigSource)

	if _, err := webui.Embedded(); err != nil {
		fmt.Fprintf(stderr, "[FAIL] dashboard: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "[PASS] dashboard: embedded assets are available")

	cfg := resolution.Config
	databaseDirectory := filepath.Dir(cfg.Storage.DBPath)
	if info, err := os.Stat(databaseDirectory); err == nil && info.IsDir() {
		fmt.Fprintf(stdout, "[PASS] data directory: %s\n", databaseDirectory)
	} else if os.IsNotExist(err) {
		fmt.Fprintf(stdout, "[WARN] data directory: %s will be created on start\n", databaseDirectory)
	} else if err != nil {
		fmt.Fprintf(stderr, "[FAIL] data directory: %v\n", err)
		return 1
	}
	schema, err := service.InspectSQLiteSchema(cfg.Storage.DBPath)
	if err != nil {
		fmt.Fprintf(stderr, "[FAIL] database schema: %v\n", err)
		return 1
	}
	if !schema.Exists {
		fmt.Fprintf(stdout, "[WARN] database schema: database will be created at v%d on start\n", schema.TargetVersion)
	} else if schema.MigrationRequired {
		fmt.Fprintf(stdout, "[WARN] database schema: v%d will be backed up and upgraded to v%d on start\n", schema.Version, schema.TargetVersion)
	} else {
		fmt.Fprintf(stdout, "[PASS] database schema: v%d\n", schema.Version)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Server.Host, cfg.Server.Port))
	if err != nil {
		fmt.Fprintf(stderr, "[FAIL] listen address: %v\n", err)
		return 1
	}
	_ = listener.Close()
	fmt.Fprintf(stdout, "[PASS] listen address: %s\n", net.JoinHostPort(cfg.Server.Host, cfg.Server.Port))
	info := buildinfo.Current()
	fmt.Fprintf(stdout, "[PASS] build: %s (%s, %s)\n", info.Version, info.Commit, info.BuildTime)
	return 0
}

func displayConfigPath(resolution *config.Resolution) string {
	if resolution.ConfigPath == "" {
		return resolution.Standard.ConfigFile + " (not present; defaults and environment are valid)"
	}
	return resolution.ConfigPath
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Agent Context Probe")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  agent-context-probe start [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]")
	fmt.Fprintln(output, "  agent-context-probe doctor [--config FILE] [--data-dir DIR] [--host HOST] [--port PORT]")
	fmt.Fprintln(output, "  agent-context-probe version")
}
