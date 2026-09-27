package models

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUserDoesNotPersistCatalog(t *testing.T) {
	for _, tc := range []struct {
		email, legacy string
	}{
		{"student@gatech.edu", "activities"},
		{"STUDENT@GATECH.EDU", "activities"},
		{"student@gatech.edu.example", "activities"},
		{"student@alumni.gatech.edu", "demo_activities"},
		{"student@example.com", "demo_activities"},
		{"", "demo_activities"},
	} {
		t.Run(tc.email, func(t *testing.T) {
			raw, err := bson.Marshal(bson.M{"email": tc.email, "catalog": tc.legacy})
			if err != nil {
				t.Fatal(err)
			}
			var user User
			if err := bson.Unmarshal(raw, &user); err != nil {
				t.Fatal(err)
			}
			raw, err = bson.Marshal(user)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bson.Raw(raw).LookupErr("catalog"); err == nil {
				t.Fatal("user writes the obsolete catalog setting")
			}
		})
	}
}
