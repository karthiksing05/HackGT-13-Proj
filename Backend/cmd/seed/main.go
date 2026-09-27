// Command seed fills a SideQuests database with showcase people and
// sidequests, so the app is lively the first time someone opens it:
//
//   - Atlanta, what every regular account sees: eight people (role
//     "showcase") with preferences and taste vectors, friendships among
//     them, five shared plans on today's and tomorrow's evenings around
//     Midtown, Tech Square and the BeltLine (real pitch_activities places),
//     each with a group chat, and three "free now" posts near Tech Square;
//   - Saltlight, what the demo account Sandy Byte sees: her bots (role bot;
//     Marin Okafor and Theo Park are reused when they exist), friendships
//     with her, a pending friend request, three open plans near Seaside
//     Market Square, a group plan she is in, and two "free now" posts.
//
// It is a dry run unless --apply is given. Every document it writes carries
// seed: "showcase-v1" and has a fixed id, so a rerun rewrites the same
// documents with fresh times (keeping anyone who joined a showcase plan),
// and --remove (with --apply) deletes those, plus what exists only inside
// its plans and chats, as when a host deletes a plan. It never changes a
// document it did not write, Sandy's account included.
//
//	go run ./cmd/seed [--apply] [--remove] [--world atlanta|saltlight|all] [--demo-email ADDRESS]
//	go run ./cmd/seed --history @handle[=outdoors|nightlife|arts],… [--apply] [--remove]
//
// --history gives real accounts a believable past instead (history.go):
// finished sidequests with rated and unrated stops and liked categories
// that agree with them, all reversible with --remove.
//
// It reads the server's environment names: MONGO_URI (default
// mongodb://127.0.0.1:27017, e.g. the SSH tunnel to the server), MONGO_DB
// (required with --apply; a dry run defaults to freetime), DEMO_DATE
// (default 2026-09-27) and DEMO_TZ (default America/New_York) for
// Saltlight's clock, and ML_SERVICE_URL (default http://127.0.0.1:8000) for
// taste vectors.
package main

import (
	"Backend/pkg/datastore"
	"Backend/pkg/democlock"
	"Backend/pkg/ml"
	"Backend/pkg/store"
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Defaults of the environment (the server's, pkg/config; DEMO_DATE is
// production's).
const (
	defaultMongoURI = "mongodb://127.0.0.1:27017"
	defaultMongoDB  = "freetime"
	defaultDemoDate = "2026-09-27"
	defaultMLURL    = ml.DefaultBaseURL
	atlantaTZ       = "America/New_York"
)

// Options is one run of the seed.
type Options struct {
	Apply  bool
	Remove bool
	// Worlds run in this order; Named is set when --world named one, so a
	// world that cannot be seeded is an error rather than a note.
	Worlds    []string
	Named     bool
	History   []historySel // --history: the people to give a past (no worlds then)
	DemoEmail string
	Demo      *democlock.Clock // Saltlight's clock (DEMO_DATE, DEMO_TZ)
	Atlanta   *time.Location
	ML        *ml.Client // nil: taste vectors are blended from the catalogs
	MLURL     string
	Now       time.Time // the wall clock of the run
	Target    string    // host and database, as printed (no credentials)
}

func main() {
	os.Exit(cli(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

const usage = `usage: go run ./cmd/seed [--apply] [--remove] [--world atlanta|saltlight|all] [--demo-email ADDRESS]
       go run ./cmd/seed --history @handle[=outdoors|nightlife|arts],… [--apply] [--remove]

Fills the database with showcase people and sidequests (a dry run unless --apply).
--history gives the named real accounts past sidequests, ratings and interests instead.
--remove deletes what it wrote instead and restores what it changed (also a dry run unless --apply).

Environment: MONGO_URI (default mongodb://127.0.0.1:27017), MONGO_DB (required with --apply),
DEMO_DATE (default 2026-09-27), DEMO_TZ (default America/New_York), ML_SERVICE_URL (default http://127.0.0.1:8000).
`

func cli(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	apply := fs.Bool("apply", false, "write (without it nothing is written)")
	remove := fs.Bool("remove", false, "delete what this seed wrote instead")
	world := fs.String("world", "all", "atlanta, saltlight or all")
	demoEmail := fs.String("demo-email", defaultDemoEmail, "the demo account the Saltlight world is built around")
	history := fs.String("history", "", "give these real accounts a past instead: @handle[=outdoors|nightlife|arts],…")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	o, uri, db, err := readOptions(getenv, *apply, *remove, *world, *demoEmail)
	if err == nil && *history != "" {
		if *world != "all" {
			err = fmt.Errorf("--history and --world do not go together")
		} else {
			o.History, err = parseHistory(*history)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "seed: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	client, err := datastore.Connect(ctx, uri)
	if err != nil {
		fmt.Fprintf(stderr, "seed: cannot reach MongoDB at %s: %s\n", o.Target, redact(err.Error(), uri))
		return 1
	}
	defer datastore.Disconnect(client)
	st := store.New(client.Database(db), time.Now)
	if err := Run(ctx, st, o, stdout); err != nil {
		fmt.Fprintf(stderr, "seed: %s\n", redact(err.Error(), uri))
		return 1
	}
	return 0
}

// readOptions reads the flags and the environment. --apply needs MONGO_DB set
// explicitly, so a write never lands in a default database.
func readOptions(getenv func(string) string, apply, remove bool, world, demoEmail string) (Options, string, string, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	o := Options{Apply: apply, Remove: remove, DemoEmail: strings.TrimSpace(demoEmail), Now: time.Now()}
	switch world {
	case "all":
		o.Worlds = []string{worldAtlanta, worldSaltlight}
	case worldAtlanta, worldSaltlight:
		o.Worlds, o.Named = []string{world}, true
	default:
		return o, "", "", fmt.Errorf("--world must be atlanta, saltlight or all, got %q", world)
	}
	uri := get("MONGO_URI", defaultMongoURI)
	db := get("MONGO_DB", "")
	if db == "" {
		if apply {
			return o, "", "", fmt.Errorf("MONGO_DB must be set explicitly with --apply (the server's database, e.g. MONGO_DB=%s)", defaultMongoDB)
		}
		db = defaultMongoDB
	}
	o.Target = fmt.Sprintf("%s, database %s", targetHost(uri), db)
	var err error
	if o.Demo, err = democlock.New(get("DEMO_DATE", defaultDemoDate), get("DEMO_TZ", democlock.DefaultTZ)); err != nil {
		return o, "", "", err
	}
	if o.Atlanta, err = time.LoadLocation(atlantaTZ); err != nil {
		return o, "", "", err
	}
	o.MLURL = strings.TrimRight(get("ML_SERVICE_URL", get("ML_API_URL", defaultMLURL)), "/")
	o.ML = ml.NewClient(o.MLURL)
	return o, uri, db, nil
}

// targetHost is the host part of a MongoDB URI, never its credentials.
func targetHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" {
		return "the MONGO_URI host"
	}
	return u.Scheme + "://" + u.Host
}

// redact keeps the URI and its password out of an error message.
func redact(msg, uri string) string {
	msg = strings.ReplaceAll(msg, uri, "<MONGO_URI>")
	if u, err := url.Parse(uri); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
	}
	return msg
}

// appCollections are what the server creates at startup (its indexes): a
// database without them is not the app's.
var appCollections = []string{store.CollUsers, store.CollItineraries, store.CollThreads, store.CollMessages,
	store.CollFriendships, store.CollFriendRequests, store.CollForumPosts}

func worldByName(name string) *worldSpec {
	for _, w := range worlds {
		if w.name == name {
			return w
		}
	}
	return nil
}

// Run is the seed against st, reporting to out.
func Run(ctx context.Context, st *store.Store, o Options, out io.Writer) error {
	p := printer{w: out}
	p.header(o)
	names, err := st.DB().ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	want := slices.Clone(appCollections)
	switch {
	case len(o.History) > 0:
		want = []string{store.CollUsers, store.CollItineraries, store.CollRatings}
		if !o.Remove {
			want = append(want, store.DefaultCatalog)
		}
	case !o.Remove:
		for _, name := range o.Worlds {
			want = append(want, worldByName(name).catalog)
		}
	}
	var missing []string
	for _, coll := range want {
		if !slices.Contains(names, coll) {
			missing = append(missing, coll)
		}
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("the target has no %s collection (is MONGO_DB the server's database?)", strings.Join(missing, ", "))
		if o.Apply {
			return fmt.Errorf("refusing to write: %s", msg)
		}
		p.f("Warning: %s; --apply would refuse", msg)
	}
	if len(o.History) > 0 {
		return runHistory(ctx, st, o, p)
	}
	if o.Remove {
		return runRemove(ctx, st, o, p)
	}

	taste := checkTaste(ctx, o.ML, o.MLURL)
	p.f("Taste vectors: %s", taste.line)
	var plans []*worldPlan
	for _, name := range o.Worlds {
		wp, err := planWorld(ctx, st, worldByName(name), o, taste)
		if err != nil {
			return err
		}
		p.world(wp)
		if wp.skipped != "" && o.Named {
			return fmt.Errorf("%s: %s", name, wp.skipped)
		}
		if wp.skipped == "" {
			plans = append(plans, wp)
		}
	}
	t := totals(plans)
	p.f("")
	p.f("Total: %s", t)
	if !o.Apply {
		p.f("Dry run: nothing was written. Run again with --apply to write it.")
		return nil
	}
	if t.conflict > 0 {
		return fmt.Errorf("refusing to write: %d conflict(s), marked ! above; nothing was written", t.conflict)
	}
	p.f("Writing to %s", o.Target)
	for _, wp := range plans {
		tally, err := wp.write(ctx, st, taste)
		if err != nil {
			return err
		}
		p.written(wp, tally)
	}
	p.f("Done. Rerun it any time to refresh the times; --remove --apply takes it all away again.")
	return nil
}

func runRemove(ctx context.Context, st *store.Store, o Options, p printer) error {
	var removals []*removal
	for _, name := range o.Worlds {
		r, err := planRemoval(ctx, st, name)
		if err != nil {
			return err
		}
		p.removal(r)
		removals = append(removals, r)
	}
	p.f("")
	if !o.Apply {
		p.f("Dry run: nothing was deleted. Run again with --remove --apply to delete it.")
		return nil
	}
	for _, r := range removals {
		if err := r.apply(ctx, st); err != nil {
			return err
		}
	}
	p.f("Deleted. Everything else in %s is as it was.", o.Target)
	return nil
}
