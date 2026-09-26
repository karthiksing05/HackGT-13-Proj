package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"context"
	"sync/atomic"
	"time"
)

// Social is what this package borrows from pkg/api/social (backend-C),
// which owns the ChatThread and ForumPost views: thread.updated after a
// member leaves a plan, and the forum posts of GET /search. social.Register
// installs it with UseSocial(…). Until then a leave sends no thread.updated
// (the thread itself is still updated) and search returns no posts.
type Social interface {
	// ThreadChanged sends thread.updated for a thread to each of its
	// members, rendered for them.
	ThreadChanged(ctx context.Context, d *api.Deps, threadID string, tz *time.Location)
	// SearchPosts is the forum posts viewer may see whose title, text or
	// author's name contains q.
	SearchPosts(ctx context.Context, d *api.Deps, viewer *models.User, q string, tz *time.Location) ([]contract.ForumPost, error)
}

type socialBox struct{ s Social }

var socialViews atomic.Pointer[socialBox]

// UseSocial installs the social views; nil removes them. The
// implementation must be stateless (it receives Deps on every call), since
// every router built in a process shares it.
func UseSocial(s Social) {
	if s == nil {
		socialViews.Store(nil)
		return
	}
	socialViews.Store(&socialBox{s: s})
}

func currentSocial() Social {
	if box := socialViews.Load(); box != nil {
		return box.s
	}
	return nil
}
