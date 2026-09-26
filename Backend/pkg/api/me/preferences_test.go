package me_test

import (
	"Backend/pkg/api/me"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestPreferencesRoundTripAndDefaults(t *testing.T) {
	p := &testutil.ProfilesRecorder{}
	srv := testutil.New(t, testutil.WithProfiles(p))
	a := srv.Signup(t, "Pref Person")
	b := srv.Signup(t, "Other Person")

	// Before the first save: the defaults, exactly.
	res := srv.Do(t, "GET", "/me/preferences", nil, a).Expect(t, http.StatusOK)
	want, _ := json.Marshal(contract.DefaultPreferences())
	if canonical(t, res.Body) != canonical(t, want) {
		t.Fatalf("defaults:\n%s\nwant\n%s", res.Body, want)
	}

	// PUT the contract example: the answer and a later GET are that example.
	body := example(t, "Preferences")
	saved := putJSON(t, srv, "PUT", "/me/preferences", body, a).Expect(t, http.StatusOK)
	if canonical(t, saved.Body) != canonical(t, body) {
		t.Fatalf("PUT answer:\n%s\nwant\n%s", saved.Body, body)
	}
	got := srv.Do(t, "GET", "/me/preferences", nil, a).Expect(t, http.StatusOK)
	if canonical(t, got.Body) != canonical(t, body) {
		t.Fatalf("GET after PUT:\n%s", got.Body)
	}
	var user contract.User
	srv.Do(t, "GET", "/me", nil, a).Expect(t, http.StatusOK).JSON(t, &user)
	if !user.SetupComplete {
		t.Fatal("the first save must finish setup")
	}
	// The taste vectors were refreshed once, synchronously.
	if n := p.Refreshes(a.UserID); n != 1 {
		t.Fatalf("profile refreshes: %d", n)
	}

	// B still sees the defaults.
	res = srv.Do(t, "GET", "/me/preferences", nil, b).Expect(t, http.StatusOK)
	if canonical(t, res.Body) != canonical(t, want) {
		t.Fatalf("B sees A's preferences: %s", res.Body)
	}
	srv.Do(t, "GET", "/me", nil, b).Expect(t, http.StatusOK).JSON(t, &user)
	if user.SetupComplete || p.Refreshes(b.UserID) != 0 {
		t.Fatal("A's save touched B")
	}

	// camelCase keys, the "chill" label, answers trimmed, unknown answers
	// dropped, empty enums defaulted.
	raw := `{"ratings":{"liveMusic":5,"bigCrowds":1},"pace":"chill","answers":{"perfectAfternoon":"  a long walk  ","neverDo":"","favoriteColor":"green"},"instant_checkout":true,"instant_checkout_limit_cents":2500}`
	var out contract.Preferences
	putJSON(t, srv, "PUT", "/me/preferences", []byte(raw), a).Expect(t, http.StatusOK).JSON(t, &out)
	wantOut := contract.DefaultPreferences()
	wantOut.Ratings = contract.Ratings{"live_music": 5, "big_crowds": 1}
	wantOut.Pace = contract.PaceRelaxed
	wantOut.PreferFree = false // omitted booleans are false: a PUT is the whole object
	wantOut.Answers = contract.Answers{"perfect_afternoon": "a long walk"}
	wantOut.InstantCheckout = true
	wantOut.InstantCheckoutLimitCents = 2500
	if !reflect.DeepEqual(out, wantOut) {
		t.Fatalf("normalized save:\n%+v\nwant\n%+v", out, wantOut)
	}
	stored, err := srv.Store.Users().ByID(context.Background(), a.UserID)
	if err != nil || stored.Prefs.Answers["perfect_afternoon"] != "a long walk" || len(stored.Prefs.Answers) != 1 || stored.Prefs.Ratings["live_music"] != 5 {
		t.Fatalf("stored prefs: %v %+v", err, stored.Prefs)
	}
}

func TestPreferencesValidation(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Strict Person")
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), a).Expect(t, http.StatusOK)
	before := srv.Do(t, "GET", "/me/preferences", nil, a).Expect(t, http.StatusOK).Body

	enums := "Pick one of the offered options for company, pace, spend, flexibility and split style."
	ratings := "Ratings are 1 to 5 for the trip types offered."
	cases := []struct{ body, msg string }{
		{`{"company":"crowd"}`, enums},
		{`{"pace":"sprint"}`, enums},
		{`{"spend":"lots"}`, enums},
		{`{"flexibility":"whatever"}`, enums},
		{`{"split_style":"never"}`, enums},
		{`{"ratings":{"outdoors":6}}`, ratings},
		{`{"ratings":{"outdoors":0}}`, ratings},
		{`{"ratings":{"skiing":3}}`, ratings},
		{`{"instant_checkout_limit_cents":-1}`, "The instant checkout limit can't be negative."},
		{`{"answers":{"plan_around":"` + strings.Repeat("x", 2001) + `"}}`, me.MsgAnswerTooLong},
		{`{"ratings":`, "Check the details and try again."},
		{`{"ratings":{"food":"five"}}`, "Check the details and try again."},
	}
	for _, tc := range cases {
		res := putJSON(t, srv, "PUT", "/me/preferences", []byte(tc.body), a)
		if res.Status != http.StatusBadRequest || res.Message() != tc.msg {
			t.Errorf("%s → %d %q, want 400 %q", tc.body[:min(len(tc.body), 40)], res.Status, res.Message(), tc.msg)
		}
	}
	// An answer of exactly 2,000 characters is fine.
	putJSON(t, srv, "PUT", "/me/preferences", []byte(`{"answers":{"plan_around":"`+strings.Repeat("é", 2000)+`"}}`), a).Expect(t, http.StatusOK)
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), a).Expect(t, http.StatusOK)
	if after := srv.Do(t, "GET", "/me/preferences", nil, a).Body; string(after) != string(before) {
		t.Fatalf("round trip drifted:\n%s\n%s", before, after)
	}

	if res := srv.Do(t, "GET", "/me/preferences", nil, nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Status)
	}
	if res := srv.Do(t, "PUT", "/me/preferences", contract.DefaultPreferences(), nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Status)
	}
}

func TestPreferencesSaveSurvivesProfileFailure(t *testing.T) {
	ctx := context.Background()

	// ML reachable: the refresh runs and the stored profile hash is left to it.
	ok := &testutil.ProfilesRecorder{}
	srv := testutil.New(t, testutil.WithProfiles(ok))
	a := srv.Signup(t, "Hash Keeper")
	if _, err := srv.Store.Users().Update(ctx, a.UserID, bson.M{"profileTextHash": "profile-v1:abc"}); err != nil {
		t.Fatal(err)
	}
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), a).Expect(t, http.StatusOK)
	if u, _ := srv.Store.Users().ByID(ctx, a.UserID); u.ProfileTextHash != "profile-v1:abc" || ok.Refreshes(a.UserID) != 1 {
		t.Fatalf("a successful refresh must not clear the hash: %q, %d refreshes", u.ProfileTextHash, ok.Refreshes(a.UserID))
	}

	// ML down: the save still succeeds, the old vectors stay and the hash is
	// cleared so the next ranking rebuilds the profile.
	down := &testutil.ProfilesRecorder{Err: errors.New("ml down")}
	srv = testutil.New(t, testutil.WithProfiles(down))
	b := srv.Signup(t, "Hash Loser")
	vector := []float64{0.1, 0.2}
	if _, err := srv.Store.Users().Update(ctx, b.UserID, bson.M{"profileTextHash": "profile-v1:abc", "positiveEmbedding": vector}); err != nil {
		t.Fatal(err)
	}
	var out contract.Preferences
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), b).Expect(t, http.StatusOK).JSON(t, &out)
	if out.Ratings["outdoors"] != 5 || down.Refreshes(b.UserID) != 1 {
		t.Fatalf("save with ML down: %+v, %d refreshes", out, down.Refreshes(b.UserID))
	}
	u, _ := srv.Store.Users().ByID(ctx, b.UserID)
	if u.ProfileTextHash != "" || !reflect.DeepEqual(u.PositiveEmbedding, vector) || !u.SetupComplete || u.Prefs.Ratings["outdoors"] != 5 {
		t.Fatalf("after a failed refresh: hash %q, vector %v, setup %v, prefs %+v", u.ProfileTextHash, u.PositiveEmbedding, u.SetupComplete, u.Prefs)
	}

	// No Profiles wired (ML not merged yet): the save works.
	srv = testutil.New(t)
	c := srv.Signup(t, "No Profiles")
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), c).Expect(t, http.StatusOK)
}
