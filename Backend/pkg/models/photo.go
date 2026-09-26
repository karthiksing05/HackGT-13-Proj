package models

import "time"

// Photo kinds.
const (
	PhotoKindAvatar = "avatar"
	PhotoKindGroup  = "group"
)

// Photo is stored JPEG/PNG bytes (photos, ≤ 2 MB), served at GET /photos/{id}.
type Photo struct {
	ID          string    `bson:"_id"` // uuid, unguessable
	OwnerID     string    `bson:"ownerId"`
	Kind        string    `bson:"kind"` // avatar | group
	GroupID     string    `bson:"groupId,omitempty"`
	ContentType string    `bson:"contentType"`
	Bytes       []byte    `bson:"bytes"`
	Size        int       `bson:"size"`
	CreatedAt   time.Time `bson:"createdAt"`
}
