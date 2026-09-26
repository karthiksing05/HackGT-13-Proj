//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type joinUpdate struct {
	PostID string              `json:"post_id"`
	Result contract.JoinResult `json:"result"`
}

type itineraryRemoved struct {
	ItineraryID string `json:"itinerary_id"`
}

type messageNew struct {
	ThreadID string           `json:"thread_id"`
	Message  contract.Message `json:"message"`
}

// planPost is the viewer's rendering of a plan post in their feed (area is
// extra query parameters, "&lat=…&lng=…" for someone without a home base).
func planPost(t testing.TB, s *Session, id string, area ...string) contract.ForumPost {
	t.Helper()
	for _, p := range forum(t, s, "?type=plans"+strings.Join(area, "")) {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("plan %s is not in %s's feed", id, s.Label)
	return contract.ForumPost{}
}

// TestDemoJoinMarinsPlan joins Marin's open plan from the Forum, uses its
// group chat and notes, and leaves again both ways (cancel join, leave).
func TestDemoJoinMarinsPlan(t *testing.T) {
	s := sandy(t)
	join := "/forum/posts/" + seedOpenPlan + "/join-requests"
	do(t, s, "DELETE", join, nil).NoContent(t) // start outside the plan
	before := planPost(t, s, seedOpenPlan)
	if before.JoinStatus != contract.JoinNone || before.SpotsLeft == nil || before.ThreadID != nil {
		t.Fatalf("before joining: %+v", before)
	}
	ws := dial(t, s)

	res := send[contract.JoinResult](t, s, "POST", join, nil, http.StatusOK)
	if res.Status != contract.JoinJoined || res.ItineraryID == nil || *res.ItineraryID != seedOpenPlan || res.ThreadID == nil || *res.ThreadID == "" {
		t.Fatalf("join result: %+v", res)
	}
	threadID := *res.ThreadID

	var ju joinUpdate
	expectEvent(t, ws, "join.update", 10*time.Second, &ju, func(v joinUpdate) bool { return v.PostID == seedOpenPlan })
	if ju.Result.Status != contract.JoinJoined || ju.Result.ThreadID == nil || *ju.Result.ThreadID != threadID {
		t.Errorf("join.update: %+v", ju)
	}
	var itEv contract.Itinerary
	expectEvent(t, ws, "itinerary.updated", 10*time.Second, &itEv, func(v contract.Itinerary) bool { return v.ID == seedOpenPlan })
	if itEv.IsHost || itEv.GoingCount != before.GoingCount+1 || itEv.Title != "Golden hour by the market" {
		t.Errorf("itinerary.updated for the joiner: host %v, going %d, %q", itEv.IsHost, itEv.GoingCount, itEv.Title)
	}
	var thEv contract.ChatThread
	expectEvent(t, ws, "thread.updated", 10*time.Second, &thEv, func(v contract.ChatThread) bool { return v.ID == threadID })
	if !thEv.IsGroup || thEv.Title != "Golden hour by the market" || len(thEv.Members) != before.GoingCount+1 {
		t.Errorf("thread.updated: %+v", thEv)
	}
	var forumEv struct{}
	expectEvent(t, ws, "forum.update", 10*time.Second, &forumEv, nil)

	active := get[[]contract.Itinerary](t, s, "/itineraries?status=active")
	at := slices.IndexFunc(active, func(it contract.Itinerary) bool { return it.ID == seedOpenPlan })
	if at < 0 {
		t.Fatalf("Marin's plan is not on Sandy's Home: %d active", len(active))
	}
	plan := active[at]
	if plan.IsHost || plan.Visibility != contract.VisibilityOpen || plan.MaxGroupSize == nil || *plan.MaxGroupSize != 6 || plan.LockAt == nil ||
		!plan.LockAt.Before(plan.Start.Time) || len(plan.Items) < 3 {
		t.Errorf("joined plan: %+v", plan)
	}
	var stops []contract.ItineraryItem
	for i, item := range plan.Items {
		if i > 0 && item.Start.Before(plan.Items[i-1].Start.Time) {
			t.Errorf("items out of order at %d", i)
		}
		if item.Kind == contract.KindGroup { // a plan with company renders its stops as group blocks
			stops = append(stops, item)
			if len(item.People)+item.ExtraGoing != plan.GoingCount {
				t.Errorf("group stop %q shows %d people, %d are going", item.Title, len(item.People), plan.GoingCount)
			}
		}
		if item.Start.Before(plan.Start.Time) || item.End.After(plan.BackBy.Time) {
			t.Errorf("item %q %s–%s outside the plan's window", item.Title, item.Start, item.End)
		}
	}
	if len(stops) == 0 {
		t.Fatal("no stops in Marin's plan")
	}
	if got := get[contract.Itinerary](t, s, "/itineraries/"+seedOpenPlan); got.ID != seedOpenPlan || got.GoingCount != plan.GoingCount {
		t.Errorf("GET joined plan: %+v", got.ID)
	}
	post := planPost(t, s, seedOpenPlan)
	if post.JoinStatus != contract.JoinJoined || post.ThreadID == nil || *post.ThreadID != threadID || *post.SpotsLeft != *before.SpotsLeft-1 ||
		post.GoingCount != before.GoingCount+1 {
		t.Errorf("post after joining: status %s, thread %v, spots %v, going %d", post.JoinStatus, post.ThreadID, post.SpotsLeft, post.GoingCount)
	}
	threads := get[[]contract.ChatThread](t, s, "/threads")
	if !slices.ContainsFunc(threads, func(th contract.ChatThread) bool { return th.ID == threadID && th.IsGroup }) {
		t.Error("the plan's group chat is not in Groups")
	}
	day := localDay(plan.Start.Time, config(t).Loc).Format("2006-01-02")
	days := get[[]contract.CalendarDay](t, s, "/calendar/days?from="+day+"&to="+day)
	if len(days) != 1 || !slices.ContainsFunc(days[0].Items, func(c contract.CalendarItem) bool {
		return c.ItineraryID != nil && *c.ItineraryID == seedOpenPlan
	}) {
		t.Errorf("the joined plan is not on the calendar for %s", day)
	}

	// A member cannot edit or delete the host's plan.
	title := "Mine now"
	if msg := fails(t, s, "PATCH", "/itineraries/"+seedOpenPlan, contract.ItineraryUpdate{Title: &title}, http.StatusForbidden); msg != "Only the host can edit this sidequest." {
		t.Errorf("member PATCH: %q", msg)
	}
	fails(t, s, "DELETE", "/itineraries/"+seedOpenPlan, nil, http.StatusForbidden)

	// A shared note shows on the stop; clearing it takes it back.
	stop := stops[0]
	shared := contract.NotesShared
	itemPath := "/itineraries/" + seedOpenPlan + "/items/" + stop.ID
	do(t, s, "PATCH", itemPath, contract.ItemNotesPatch{Notes: "Bring a jacket for the pier.", NotesScope: &shared}).NoContent(t)
	detail := get[contract.ItineraryItem](t, s, "/events/"+stop.ID)
	if detail.Notes == nil || *detail.Notes != "Bring a jacket for the pier." || detail.NotesScope == nil || *detail.NotesScope != contract.NotesShared {
		t.Errorf("shared note: %v %v", detail.Notes, detail.NotesScope)
	}
	do(t, s, "PATCH", itemPath, contract.ItemNotesPatch{Notes: "", NotesScope: &shared}).NoContent(t)
	if cleared := get[contract.ItineraryItem](t, s, "/events/"+stop.ID); cleared.Notes != nil {
		t.Errorf("note after clearing: %q", *cleared.Notes)
	}

	// The group chat works for the joiner.
	text := "On my way from the square! " + randomHex(2)
	msg := send[contract.Message](t, s, "POST", "/threads/"+threadID+"/messages", contract.NewMessage{Text: text}, http.StatusCreated)
	var mn messageNew
	expectEvent(t, ws, "message.new", 10*time.Second, &mn, func(v messageNew) bool { return v.Message.ID == msg.ID })
	if mn.ThreadID != threadID || mn.Message.SenderName != "You" {
		t.Errorf("message.new in the plan chat: %+v", mn)
	}

	// Joining again changes nothing.
	again := send[contract.JoinResult](t, s, "POST", join, nil, http.StatusOK)
	if again.Status != contract.JoinJoined || again.ThreadID == nil || *again.ThreadID != threadID {
		t.Errorf("second join: %+v", again)
	}
	expectNoEvent(t, ws, "join.update", time.Second, nil)

	// Cancel the join: off the plan and out of its chat.
	do(t, s, "DELETE", join, nil).NoContent(t)
	var removed itineraryRemoved
	expectEvent(t, ws, "itinerary.removed", 10*time.Second, &removed, func(v itineraryRemoved) bool { return v.ItineraryID == seedOpenPlan })
	fails(t, s, "GET", "/itineraries/"+seedOpenPlan, nil, http.StatusNotFound)
	fails(t, s, "GET", "/threads/"+threadID, nil, http.StatusNotFound)
	fails(t, s, "POST", "/threads/"+threadID+"/messages", contract.NewMessage{Text: "still here?"}, http.StatusNotFound)
	if post := planPost(t, s, seedOpenPlan); post.JoinStatus != contract.JoinNone || *post.SpotsLeft != *before.SpotsLeft || post.ThreadID != nil {
		t.Errorf("post after cancelling: %s, spots %v", post.JoinStatus, post.SpotsLeft)
	}
	for _, it := range get[[]contract.Itinerary](t, s, "/itineraries") {
		if it.ID == seedOpenPlan {
			t.Error("the plan is still on Home after cancelling")
		}
	}

	// Join again (the same chat comes back), then leave from Home.
	back := send[contract.JoinResult](t, s, "POST", join, nil, http.StatusOK)
	if back.Status != contract.JoinJoined || back.ThreadID == nil || *back.ThreadID != threadID {
		t.Errorf("rejoin: %+v", back)
	}
	if hist := get[[]contract.Message](t, s, "/threads/"+threadID+"/messages"); !slices.ContainsFunc(hist, func(m contract.Message) bool { return m.ID == msg.ID }) {
		t.Error("the chat history is gone after rejoining")
	}
	do(t, s, "POST", "/itineraries/"+seedOpenPlan+"/leave", nil).NoContent(t)
	expectEvent(t, ws, "itinerary.removed", 10*time.Second, &removed, func(v itineraryRemoved) bool { return v.ItineraryID == seedOpenPlan })
	fails(t, s, "POST", "/itineraries/"+seedOpenPlan+"/leave", nil, http.StatusNotFound)
	do(t, s, "DELETE", join, nil).NoContent(t)
	if post := planPost(t, s, seedOpenPlan); post.JoinStatus != contract.JoinNone || *post.SpotsLeft != *before.SpotsLeft {
		t.Errorf("post after leaving: %s, spots %v", post.JoinStatus, post.SpotsLeft)
	}
	fails(t, s, "POST", "/forum/posts/no-such-plan/join-requests", nil, http.StatusNotFound)
}
