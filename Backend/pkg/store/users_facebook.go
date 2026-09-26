package store

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// SetFacebookInterests stores the interests the latest Facebook import found
// (users.facebookInterests), which the profile refresh folds into the taste
// vectors; nil or empty removes them.
func (u Users) SetFacebookInterests(ctx context.Context, id string, interests []string) error {
	if len(interests) == 0 {
		return u.Unset(ctx, id, "facebookInterests")
	}
	_, err := u.Update(ctx, id, bson.M{"facebookInterests": interests})
	return err
}
