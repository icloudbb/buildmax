// Package main is the entry point for the BuildMax HTTP server (backend for portal).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	// Embed the IANA timezone database in the binary so a schedule's timezone
	// (e.g. "Asia/Shanghai") resolves through time.LoadLocation even in a
	// container image that ships no /usr/share/zoneinfo. Without it the server
	// accepts only "UTC" and rejects every named zone as unknown. See
	// docs/design/scheduled-agent-execution.md.
	_ "time/tzdata"

	"github.com/icloudbb/buildmax/internal/bootstrap"
	"github.com/icloudbb/buildmax/internal/config"
	log "github.com/icloudbb/buildmax/internal/infra/log"
)

func main() {
	sc, _ := config.LoadServerConfig()
	log.Init(log.LogConfig{LogsDir: config.LogsDir(), Level: config.LogLevel(sc.LogLevel), Filename: "buildmax-server.log", AlsoStdout: true})

	ctx := context.Background()

	// `user` is an operator subcommand rather than a separate binary: it needs
	// the same server.yaml and the same database the server uses, so it belongs
	// wherever the server is already installed and configured.
	if len(os.Args) > 1 && os.Args[1] == "user" {
		if err := bootstrap.RunUserCommand(ctx, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `model` edits the managed model catalog, which holds provider
	// credentials. It runs here, next to the database, rather than through any
	// client.
	if len(os.Args) > 1 && os.Args[1] == "model" {
		if err := bootstrap.RunModelCommand(ctx, os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `admin` grants and revokes deployment-scoped authority. It is the only
	// way the first System Administrator can exist, and the way a deployment
	// that has lost every admin recovers — so it lives here, next to the
	// database, rather than behind a login it might not be able to reach.
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		if err := bootstrap.RunAdminCommand(ctx, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `space` is break glass for a shared space whose owners can all no longer
	// sign in: it recovers ownership from here, next to the database, when the
	// public Server or IdP is unavailable.
	if len(os.Args) > 1 && os.Args[1] == "space" {
		if err := bootstrap.RunSpaceCommand(ctx, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `run-token` mints one run's credential for hand-driving a worker route.
	// It signs with the deployment's key, so it runs here rather than anywhere a
	// client could reach.
	if len(os.Args) > 1 && os.Args[1] == "run-token" {
		if err := bootstrap.RunRunTokenCommand(ctx, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// `secret` moves stored rows onto the KEK file's current key. It reads the
	// same key file and database the server does, so it runs where they are.
	if len(os.Args) > 1 && os.Args[1] == "secret" {
		if err := bootstrap.RunSecretCommand(ctx, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	portFlag := flag.Int("port", 0, "port to listen on (overrides server.yaml port, default 5678)")
	flag.Usage = usage
	flag.Parse()

	if err := bootstrap.RunServer(ctx, *portFlag); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprint(out, `Usage: buildmax-server [flags]
       buildmax-server user <command> [flags]
       buildmax-server model <command> [flags]
       buildmax-server admin <command> [flags]
       buildmax-server space <command> [args]
       buildmax-server run-token <task_run_id> [flags]
       buildmax-server secret <command>

Runs the BuildMax HTTP API and the task scheduler. Configuration comes from
BUILDMAX_HOME/server.yaml.

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprint(out, "\n"+bootstrap.UserCommandUsage)
	fmt.Fprint(out, "\n"+bootstrap.ModelCommandUsage)
	fmt.Fprint(out, "\n"+bootstrap.AdminCommandUsage)
	fmt.Fprint(out, "\n"+bootstrap.SpaceCommandUsage)
	fmt.Fprint(out, "\n"+bootstrap.RunTokenCommandUsage)
	fmt.Fprint(out, "\n"+bootstrap.SecretCommandUsage)
}
