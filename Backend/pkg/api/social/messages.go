package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// messagePage is how many messages one GET returns.
const messagePage = 30

// ListMessages is GET /threads/{id}/messages?before=<message id> → a bare
// [Message], oldest first: the newest page, or the page before a message.
func (h *H) ListMessages(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().ForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	page, err := h.d.Store.Messages().PageBefore(r.Context(), th.ID, strings.TrimSpace(r.URL.Query().Get("before")), messagePage)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	senders := make([]string, 0, len(page))
	for _, m := range page {
		senders = append(senders, m.SenderID)
	}
	ppl, err := h.loadPeople(r.Context(), senders)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.Message, 0, len(page))
	for _, m := range page {
		out = append(out, messageView(m, viewerID, ppl))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// SendMessage is POST /threads/{id}/messages {text, client_id?} → 201
// Message. The same client_id again answers 200 with the message already
// sent, and nothing is posted twice.
func (h *H) SendMessage(w http.ResponseWriter, r *http.Request) {
	var req contract.NewMessage
	if !httpx.Decode(w, r, &req) {
		return
	}
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().ForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		httpx.Error(w, http.StatusBadRequest, MsgEmptyText)
		return
	}
	msg := &models.Message{ThreadID: th.ID, SenderID: viewerID, Text: text}
	if req.ClientID != nil {
		msg.ClientID = strings.TrimSpace(*req.ClientID)
	}
	sent, err := h.post(r.Context(), th, msg, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if !sent {
		status = http.StatusOK
	}
	httpx.JSON(w, status, messageView(msg, viewerID, people{})) // the sender reads "You"
}

// post stores a message in a thread and, when it is new, bumps the thread
// and tells every member: message.new with their own sender_name, then
// thread.updated. sent is false for a replayed client id (msg becomes the
// stored message).
func (h *H) post(ctx context.Context, th *models.Thread, msg *models.Message, tz *time.Location) (sent bool, err error) {
	sent, err = h.d.Store.Messages().Insert(ctx, msg)
	if err != nil || !sent {
		return sent, err
	}
	touched, err := h.d.Store.Threads().Touch(ctx, th, msg)
	if err != nil {
		return true, err
	}
	h.pushMessage(ctx, touched, msg, tz)
	return true, nil
}

// pushMessage sends message.new and thread.updated to every member.
func (h *H) pushMessage(ctx context.Context, th *models.Thread, msg *models.Message, tz *time.Location) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	kit, err := h.threadKit(ctx, []*models.Thread{th}, tz)
	if err != nil {
		logEventError(realtime.EventMessageNew, err)
		return
	}
	for _, id := range th.MemberIDs {
		realtime.MessageNew(h.d.Publish(), id, th.ID, messageView(msg, id, kit.ppl))
	}
	for _, id := range th.MemberIDs {
		realtime.ThreadUpdated(h.d.Publish(), id, kit.render(th, id))
	}
}

// MarkRead is POST /threads/{id}/read → 204: unread back to 0, and the
// reader's other devices hear thread.read (and the refreshed thread).
func (h *H) MarkRead(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().ForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	th, err = h.d.Store.Threads().MarkRead(r.Context(), th.ID, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	realtime.ThreadRead(h.d.Publish(), viewerID, th.ID)
	h.pushThread(r.Context(), th, httpx.TZ(r), []string{viewerID})
	httpx.NoContent(w)
}
