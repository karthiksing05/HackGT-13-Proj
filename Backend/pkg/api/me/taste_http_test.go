package me_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestTasteProfileEndpoint(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Taste Person")
	b := srv.Signup(t, "Plain Person")

	var fresh contract.TasteProfile
	srv.Do(t, "GET", "/me/taste-profile", nil, a).Expect(t, http.StatusOK).JSON(t, &fresh)
	if len(fresh.Bars) != 5 || fresh.Bars[0].Label != "Outdoors" || fresh.Bars[0].Value != 0.5 {
		t.Fatalf("fresh profile: %+v", fresh)
	}

	// The example preferences plus learned tags give the contract example.
	putJSON(t, srv, "PUT", "/me/preferences", example(t, "Preferences"), a).Expect(t, http.StatusOK)
	tags := bson.M{"taste.tags": bson.M{"outdoors": 0.55, "food": 0.55, "museums": 0.475, "social": 0.7, "nightlife": 0.1}}
	if _, err := srv.Store.Users().Update(context.Background(), a.UserID, tags); err != nil {
		t.Fatal(err)
	}
	res := srv.Do(t, "GET", "/me/taste-profile", nil, a).Expect(t, http.StatusOK)
	if canonical(t, res.Body) != canonical(t, example(t, "TasteProfile")) {
		t.Fatalf("taste profile:\n%s\nwant\n%s", res.Body, example(t, "TasteProfile"))
	}

	// B's profile is still the fresh one.
	var other contract.TasteProfile
	srv.Do(t, "GET", "/me/taste-profile", nil, b).Expect(t, http.StatusOK).JSON(t, &other)
	if other.Bars[0].Value != 0.5 {
		t.Fatalf("B sees A's taste: %+v", other)
	}
	if res := srv.Do(t, "GET", "/me/taste-profile", nil, nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Status)
	}
}
