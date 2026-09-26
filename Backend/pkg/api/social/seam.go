package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/itineraries"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"context"
	"time"
)

// itinerarySocial is what pkg/api/itineraries borrows from this area
// (itineraries.Social, installed by Register): thread.updated for a group
// thread after a member leaves its plan, and the forum posts of GET /search.
// It is stateless; the Deps come with every call.
type itinerarySocial struct{}

var _ itineraries.Social = itinerarySocial{}

// ThreadChanged sends thread.updated to each of the thread's members,
// rendered for them.
func (itinerarySocial) ThreadChanged(ctx context.Context, d *api.Deps, threadID string, tz *time.Location) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	th, err := d.Store.Threads().Get(ctx, threadID)
	if err != nil {
		logEventError(realtime.EventThreadUpdated, err)
		return
	}
	(&H{d: d}).pushThread(ctx, th, tz, th.MemberIDs)
}

// SearchPosts answers GET /search's posts (see the package function).
func (itinerarySocial) SearchPosts(ctx context.Context, d *api.Deps, viewer *models.User, q string, tz *time.Location) ([]contract.ForumPost, error) {
	return SearchPosts(ctx, d, viewer, q, tz)
}

// pushItinerary sends itinerary.updated to each recipient, rendered by the
// itineraries package for them (the renderer GET /itineraries/{id} uses).
func (h *H) pushItinerary(ctx context.Context, it *models.Itinerary, recipients []string) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	itineraries.PublishUpdated(ctx, h.d, it, recipients)
}
