// sidequestz-admin is the operator tool: it reads the same environment as
// the server (or --env-file /opt/backend/.env) and runs one command.
//
//	sidequestz-admin [--env-file PATH] ensure-indexes
//	sidequestz-admin [--env-file PATH] drop-ttl <collection> [--force]
//	sidequestz-admin [--env-file PATH] reset-app-data --yes [--users]
//	sidequestz-admin [--env-file PATH] seed-demo
package main

import (
	"Backend/pkg/config"
	"Backend/pkg/datastore"
	"Backend/pkg/store"
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

const usage = `usage: sidequestz-admin [--env-file PATH] <command>

commands:
  ensure-indexes                 create every index the server expects (idempotent)
  drop-ttl <collection> [--force] drop the TTL indexes of a collection ("activities" needs --force)
  reset-app-data --yes [--users] drop every app collection except the catalogs (--users adds users)
  seed-demo                      (built by the checkout agent)

The environment is the server's (MONGO_URI, MONGO_DB, JWT_SECRET, …);
--env-file loads KEY=VALUE lines first without overriding what is set.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	global := flag.NewFlagSet("sidequestz-admin", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	envFile := global.String("env-file", "", "load KEY=VALUE lines from this file first")
	global.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := global.Parse(args); err != nil {
		return 2
	}
	rest := global.Args()
	if len(rest) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if *envFile != "" {
		if err := loadEnvFile(*envFile); err != nil {
			fmt.Fprintf(os.Stderr, "env file: %v\n", err)
			return 1
		}
	}
	command, cmdArgs := rest[0], rest[1:]
	if command == "seed-demo" {
		fmt.Fprintln(os.Stderr, "seed-demo is built by the checkout agent")
		return 2
	}
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := datastore.Connect(ctx, cfg.MongoURI)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer datastore.Disconnect(client)
	st := store.New(client.Database(cfg.MongoDB), time.Now)

	switch command {
	case "ensure-indexes":
		return ensureIndexes(ctx, st, cfg.MongoDB)
	case "drop-ttl":
		return dropTTL(ctx, st, cmdArgs)
	case "reset-app-data":
		return resetAppData(ctx, st, cmdArgs)
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
	return 2
}

func ensureIndexes(ctx context.Context, st *store.Store, db string) int {
	if err := st.EnsureIndexes(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ensure-indexes: %v\n", err)
		return 1
	}
	fmt.Printf("indexes ensured on %s\n", db)
	return 0
}

func dropTTL(ctx context.Context, st *store.Store, args []string) int {
	fs := flag.NewFlagSet("drop-ttl", flag.ContinueOnError)
	force := fs.Bool("force", false, "allow the activities catalog")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: sidequestz-admin drop-ttl <collection> [--force]")
		return 2
	}
	coll := fs.Arg(0)
	if coll == store.CollActivities && !*force {
		fmt.Fprintf(os.Stderr, "refusing to drop TTL indexes on %s without --force\n", coll)
		return 1
	}
	names, err := st.TTLIndexes(ctx, coll)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drop-ttl: %v\n", err)
		return 1
	}
	if len(names) == 0 {
		fmt.Printf("no TTL indexes on %s\n", coll)
		return 0
	}
	for _, name := range names {
		if err := st.DropIndex(ctx, coll, name); err != nil {
			fmt.Fprintf(os.Stderr, "drop %s.%s: %v\n", coll, name, err)
			return 1
		}
		fmt.Printf("dropped %s.%s\n", coll, name)
	}
	return 0
}

func resetAppData(ctx context.Context, st *store.Store, args []string) int {
	fs := flag.NewFlagSet("reset-app-data", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm")
	users := fs.Bool("users", false, "also drop users")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*yes {
		fmt.Fprintln(os.Stderr, "reset-app-data drops every app collection; add --yes to confirm (--users to include users)")
		return 2
	}
	names := slices.Clone(store.AppCollections)
	if *users {
		names = append(names, store.CollUsers)
	}
	if err := st.DropCollections(ctx, names); err != nil {
		fmt.Fprintf(os.Stderr, "reset-app-data: %v\n", err)
		return 1
	}
	fmt.Printf("dropped %s (catalogs untouched)\n", strings.Join(names, ", "))
	return 0
}

// loadEnvFile sets KEY=VALUE pairs that are not already in the environment.
// Comments, blank lines, "export " prefixes and matching quotes are handled.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
