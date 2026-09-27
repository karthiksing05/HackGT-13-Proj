package planner

import (
	"Backend/pkg/models"
	"encoding/json"
	"math/rand"
	"testing"
)

// TestGenerateIsDeterministic: the same inputs give byte-identical options
// (every page) and run logs, whatever order the catalog returns them in,
// must-see picks included.
func TestGenerateIsDeterministic(t *testing.T) {
	cases := []struct {
		name string
		acts func(t *testing.T) []models.Activity
		user *UserContext
		opts reqOpts
	}{
		{"saltlight walk", saltlight, sandy(), func() reqOpts {
			o := defaultReq()
			o.tags, o.mood = []string{"Outdoors", "Food"}, "something chill outside, no bars"
			return o
		}()},
		{"saltlight with picks", saltlight, sandy(), func() reqOpts {
			o := defaultReq()
			o.tags, o.picks = []string{"Food"}, []string{phoID, jazzID}
			return o
		}()},
		{"atlanta transit", atlanta, &UserContext{ID: "jordan", Catalog: "activities", City: "atlanta",
			PositiveEmbedding: vectorOf("music", "art", "food")}, func() reqOpts {
			o := defaultReq()
			o.start, o.end = techSquare, techSquare
			o.from, o.backBy = localAt(15, 0), localAt(23, 30)
			o.rng, o.modes, o.budget, o.pace = "transit", []string{"marta", "walk"}, 3, "packed"
			o.tags = []string{"Music", "Art"}
			return o
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			once := func(acts []models.Activity) (string, string) {
				tp := newTestPlanner(acts, testConfig())
				batch, _ := tp.generate(t, c.user, c.opts)
				if len(batch.Options) == 0 {
					t.Fatalf("no options: %s", batch.Reason)
				}
				opts, err := json.Marshal(tp.allOptions(t, c.user, batch))
				if err != nil {
					t.Fatal(err)
				}
				run, err := json.Marshal(tp.run(t, batch.RunID))
				if err != nil {
					t.Fatal(err)
				}
				return string(opts), string(run)
			}
			acts := c.acts(t)
			o1, r1 := once(acts)
			o2, r2 := once(acts)
			if o1 != o2 || r1 != r2 {
				t.Fatal("two identical runs differ")
			}
			shuffled := append([]models.Activity(nil), acts...)
			rand.New(rand.NewSource(3)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			o3, r3 := once(shuffled)
			if o3 != o1 {
				t.Error("options depend on the catalog's order")
			}
			if r3 != r1 {
				t.Error("the run log depends on the catalog's order")
			}
		})
	}
}
