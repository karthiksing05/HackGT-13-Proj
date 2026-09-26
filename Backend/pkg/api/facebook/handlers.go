package facebook

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Sentences the app shows (400/409/5xx messages, or the callback's
// status=error&message=…).
const (
	MsgConnectFirst    = "Connect Facebook first."
	MsgReconnect       = "Facebook needs you to sign in again."
	MsgNotSetUp        = "Facebook isn't set up on this server yet."
	MsgNoAnswer        = "Facebook didn't answer. Try again in a moment."
	MsgBusy            = "Facebook is busy right now. Try again in a few minutes."
	MsgStateExpired    = "That sign-in link expired. Try again."
	MsgDidntConnect    = "Facebook didn't connect. Try again."
	MsgLinkedElsewhere = "That Facebook account is connected to another SideQuests account."
	MsgBadSignature    = "That request isn't signed by Facebook."
)

const (
	provider = "facebook"
	// appReturnURL is where the callback sends the browser back to the app.
	appReturnURL = "sidequestz://integrations/facebook"

	stateTTL    = 10 * time.Minute
	deletionTTL = 90 * 24 * time.Hour // how long a deletion confirmation code answers
	maxLikes    = 1000
	maxFriends  = 1000

	graphBudget         = 30 * time.Second // all Graph calls of one callback or import
	revokeBudget        = 5 * time.Second
	profileTimeout      = 15 * time.Second
	asyncProfileTimeout = 20 * time.Second
)

// requestedScopes are asked for in the Login dialog; optionalScopes are the
// ones a person can turn off there (declined_scopes lists those).
var (
	requestedScopes = []string{"public_profile", "user_likes", "user_location", "user_friends"}
	optionalScopes  = []string{"user_likes", "user_location", "user_friends"}
)

// ---- GET /integrations/facebook ------------------------------------------

// Connection is GET /integrations/facebook → FacebookConnection, with the
// last import's friends shown with their current relation to the viewer.
func (h *H) Connection(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	conn, err := h.connection(r.Context(), user.ID.Hex())
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, conn)
}

func (h *H) connection(ctx context.Context, userID string) (contract.FacebookConnection, error) {
	out := contract.FacebookConnection{DeclinedScopes: []string{}}
	fbs := h.d.Store.Facebook()
	acct, err := fbs.Account(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if acct.AccessTokenEnc == "" { // removed on Facebook (deauthorize)
		return out, nil
	}
	out.Connected = true
	out.NeedsReconnect = acct.NeedsReconnect
	out.Name = view.StrPtr(acct.Name)
	out.DeclinedScopes = knownScopes(acct.DeclinedScopes)
	imp, err := fbs.Import(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	last, err := h.importView(ctx, userID, imp)
	if err != nil {
		return out, err
	}
	out.LastImport = &last
	return out, nil
}

// ---- POST /integrations/facebook/connect ----------------------------------

// Connect is POST /integrations/facebook/connect {rerequest} → {url}: the
// Login dialog with a single-use state (10 min) bound to the caller.
func (h *H) Connect(w http.ResponseWriter, r *http.Request) {
	var req contract.FacebookConnectRequest
	if !httpx.DecodeOptional(w, r, &req) {
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if !h.ready() {
		httpx.Error(w, http.StatusServiceUnavailable, MsgNotSetUp)
		return
	}
	state, err := h.d.Store.WebSessions().Insert(r.Context(), models.WebSession{
		UserID: user.ID.Hex(), Purpose: store.PurposeFacebookState, Provider: provider, Rerequest: req.Rerequest,
	}, stateTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.URLResponse{URL: h.dialogURL(state, req.Rerequest)})
}

// dialogURL is Facebook's Login dialog; rerequest asks again for the
// permissions the person turned off.
func (h *H) dialogURL(state string, rerequest bool) string {
	q := url.Values{
		"client_id":     {h.d.Cfg.FBAppID},
		"redirect_uri":  {h.redirectURI()},
		"state":         {state},
		"response_type": {"code"},
		"scope":         {strings.Join(requestedScopes, ",")},
	}
	if rerequest {
		q.Set("auth_type", "rerequest")
	}
	return "https://www.facebook.com/" + graphVersion(h.d.Cfg.FBGraphVersion) + "/dialog/oauth?" + q.Encode()
}

// redirectURI must match the Valid OAuth Redirect URI in the Meta app.
func (h *H) redirectURI() string { return h.d.Cfg.PublicBaseURL + "/integrations/facebook/callback" }

// ---- GET /integrations/facebook/callback ----------------------------------

// Callback is where the Login dialog returns (public; the state names the
// user). It always redirects to the app: status=connected, denied, or
// error with a sentence.
func (h *H) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sess, err := h.d.Store.WebSessions().Consume(r.Context(), q.Get("state"), store.PurposeFacebookState)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Error().Err(err).Str("request_id", httpx.RequestID(r)).Msg("facebook callback: state lookup failed")
			backToApp(w, r, "error", MsgDidntConnect)
			return
		}
		backToApp(w, r, "error", MsgStateExpired)
		return
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			backToApp(w, r, "denied", "")
			return
		}
		log.Warn().Str("error", e).Str("reason", q.Get("error_reason")).Msg("facebook login returned an error")
		backToApp(w, r, "error", MsgDidntConnect)
		return
	}
	code := q.Get("code")
	if code == "" {
		backToApp(w, r, "error", MsgDidntConnect)
		return
	}
	if !h.ready() {
		backToApp(w, r, "error", MsgNotSetUp)
		return
	}
	if msg := h.link(r.Context(), sess.UserID, code); msg != "" {
		backToApp(w, r, "error", msg)
		return
	}
	backToApp(w, r, "connected", "")
}

// link finishes a Login for userID: code → short-lived token → long-lived
// token → who they are and what they granted → the sealed token stored. It
// returns the sentence to show when that fails ("" on success).
func (h *H) link(ctx context.Context, userID, code string) string {
	g := h.graph()
	gctx, cancel := context.WithTimeout(ctx, graphBudget)
	defer cancel()
	short, err := g.ExchangeCode(gctx, code, h.redirectURI())
	if err != nil {
		return callbackSentence("exchange code", userID, err)
	}
	long, err := g.LongLived(gctx, short.AccessToken)
	if err != nil {
		return callbackSentence("long-lived token", userID, err)
	}
	me, err := g.Me(gctx, long.AccessToken, false)
	if err != nil {
		return callbackSentence("me", userID, err)
	}
	perms, err := g.Permissions(gctx, long.AccessToken)
	if err != nil {
		return callbackSentence("permissions", userID, err)
	}
	granted, declined := splitScopes(perms)
	sealed, err := h.box.seal(userID, long.AccessToken)
	if err != nil {
		log.Error().Err(err).Str("user", userID).Msg("facebook: seal token")
		return MsgDidntConnect
	}

	fbs := h.d.Store.Facebook()
	prev, err := fbs.Account(ctx, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Error().Err(err).Str("user", userID).Msg("facebook: load account")
		return MsgDidntConnect
	}
	if prev != nil && prev.FBUserID != "" && prev.FBUserID != me.ID {
		// Another Facebook account than before: the old import is not theirs now.
		if err := h.dropImport(ctx, userID); err != nil {
			log.Error().Err(err).Str("user", userID).Msg("facebook: drop previous import")
			return MsgDidntConnect
		}
	}
	if other, err := fbs.AccountByFBUserID(ctx, me.ID); err == nil && other.UserID != userID {
		if other.AccessTokenEnc != "" {
			return MsgLinkedElsewhere
		}
		// Facebook already deauthorized that link: this sign-in takes it over,
		// and what was imported for the other account goes with it.
		if err := fbs.Forget(ctx, other.UserID); err != nil {
			log.Error().Err(err).Str("user", other.UserID).Msg("facebook: release a deauthorized link")
			return MsgDidntConnect
		}
		h.d.RefreshProfileAsync(other.UserID, asyncProfileTimeout)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Error().Err(err).Msg("facebook: look up fbUserId")
		return MsgDidntConnect
	}

	now := h.d.Store.Now()
	ttl := long.ExpiresIn
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	err = fbs.SaveAccount(ctx, &models.FacebookAccount{
		UserID: userID, FBUserID: me.ID, AccessTokenEnc: sealed, TokenExpiresAt: now.Add(ttl),
		GrantedScopes: granted, DeclinedScopes: declined, Name: me.Name, ConnectedAt: now,
	})
	switch {
	case errors.Is(err, store.ErrFacebookTaken):
		return MsgLinkedElsewhere
	case err != nil:
		log.Error().Err(err).Str("user", userID).Msg("facebook: save account")
		return MsgDidntConnect
	}
	return ""
}

// dropImport deletes the user's import and interests (their profile then
// refreshes without them).
func (h *H) dropImport(ctx context.Context, userID string) error {
	if err := h.d.Store.Facebook().DeleteImport(ctx, userID); err != nil {
		return err
	}
	if err := h.d.Store.Users().SetFacebookInterests(ctx, userID, nil); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	h.d.RefreshProfileAsync(userID, asyncProfileTimeout)
	return nil
}

// callbackSentence logs a failed Graph call of the Login and picks the
// sentence for the app.
func callbackSentence(step, userID string, err error) string {
	log.Warn().Err(err).Str("user", userID).Str("step", step).Msg("facebook login failed")
	var ge *GraphError
	switch {
	case RateLimited(err):
		return MsgBusy
	case errors.As(err, &ge):
		return MsgDidntConnect
	}
	return MsgNoAnswer
}

// backToApp ends the browser part of the flow with a 302 to the app's URL
// scheme, which ASWebAuthenticationSession hands to the app. Spaces are
// %20, since the app's URLComponents reads "+" literally.
func backToApp(w http.ResponseWriter, r *http.Request, status, message string) {
	target := appReturnURL + "?status=" + status
	if message != "" {
		target += "&message=" + strings.ReplaceAll(url.QueryEscape(message), "+", "%20")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}

// ---- POST /integrations/facebook/import -----------------------------------

// Import is POST /integrations/facebook/import → FacebookImport: it reads
// the Graph API, stores what it read (replacing the previous import), saves
// the interests on the user for their taste profile and refreshes it. A token
// Facebook rejects is a 409 with needs_reconnect set, never a 401 (that
// would sign the person out of SideQuests).
func (h *H) Import(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx, userID := r.Context(), user.ID.Hex()
	fbs := h.d.Store.Facebook()
	acct, err := fbs.Account(ctx, userID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusConflict, MsgConnectFirst)
		return
	case err != nil:
		api.Fail(w, r, err)
		return
	case acct.AccessTokenEnc == "":
		httpx.Error(w, http.StatusConflict, MsgConnectFirst)
		return
	case acct.NeedsReconnect:
		httpx.Error(w, http.StatusConflict, MsgReconnect)
		return
	}
	if !h.ready() {
		httpx.Error(w, http.StatusServiceUnavailable, MsgNotSetUp)
		return
	}
	token, err := h.box.open(userID, acct.AccessTokenEnc)
	if err != nil {
		log.Warn().Str("user", userID).Msg("facebook: stored token unreadable (FB_TOKEN_KEY or JWT_SECRET changed?)")
		h.needsReconnect(w, r, userID)
		return
	}
	if !acct.TokenExpiresAt.IsZero() && !h.d.Clock().Before(acct.TokenExpiresAt) {
		h.needsReconnect(w, r, userID)
		return
	}

	got, err := h.read(ctx, userID, token)
	if err != nil {
		if TokenRejected(err) {
			h.needsReconnect(w, r, userID)
			return
		}
		graphFailed(w, r, err)
		return
	}
	if err := fbs.SaveImport(ctx, got.imp); err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := fbs.UpdateProfile(ctx, userID, got.name, got.granted, got.declined); err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Users().SetFacebookInterests(ctx, userID, got.imp.Interests); err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.RefreshProfile(ctx, userID, profileTimeout); err != nil {
		log.Warn().Err(err).Str("user", userID).Msg("profile refresh after facebook import failed")
	}
	out, err := h.importView(ctx, userID, got.imp)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// readResult is what one import read from Facebook.
type readResult struct {
	imp      *models.FacebookImport
	name     string
	granted  []string
	declined []string
}

// read runs the import's Graph calls within graphBudget: permissions (so
// declined_scopes stay current), /me with the city when shared, liked Pages
// and friends on the app when granted.
func (h *H) read(ctx context.Context, userID, token string) (readResult, error) {
	g := h.graph()
	ctx, cancel := context.WithTimeout(ctx, graphBudget)
	defer cancel()
	perms, err := g.Permissions(ctx, token)
	if err != nil {
		return readResult{}, err
	}
	granted, declined := splitScopes(perms)
	me, err := g.Me(ctx, token, slices.Contains(granted, "user_location"))
	if err != nil {
		return readResult{}, err
	}
	var likes []Like
	if slices.Contains(granted, "user_likes") {
		likes, err = g.Likes(ctx, token, maxLikes)
		if PermissionMissing(err) {
			likes, err = nil, nil
			granted, declined = moveToDeclined(granted, declined, "user_likes")
		}
		if err != nil {
			return readResult{}, err
		}
	}
	var friends []Friend
	if slices.Contains(granted, "user_friends") {
		friends, err = g.Friends(ctx, token, maxFriends)
		if PermissionMissing(err) {
			friends, err = nil, nil
			granted, declined = moveToDeclined(granted, declined, "user_friends")
		}
		if err != nil {
			return readResult{}, err
		}
	}

	pages := make([]models.FacebookPage, 0, len(likes))
	for _, like := range likes {
		pages = append(pages, models.FacebookPage{ID: like.ID, Name: like.Name, Category: like.Category,
			CategoryList: like.Categories, LikedAt: like.LikedAt})
	}
	friendIDs := make([]string, 0, len(friends))
	for _, f := range friends {
		if f.ID != "" && !slices.Contains(friendIDs, f.ID) {
			friendIDs = append(friendIDs, f.ID)
		}
	}
	imp := &models.FacebookImport{
		UserID:           userID,
		ImportedAt:       h.d.Store.Now(),
		LikedPages:       len(pages),
		Pages:            pages,
		FriendFBIDs:      friendIDs,
		SuggestedRatings: suggestRatings(pages),
		Interests:        interestsFor(pages),
	}
	if me.Location != "" {
		city := me.Location
		imp.City = &city
	}
	return readResult{imp: imp, name: me.Name, granted: granted, declined: declined}, nil
}

// needsReconnect flags the connection and answers 409.
func (h *H) needsReconnect(w http.ResponseWriter, r *http.Request, userID string) {
	if err := h.d.Store.Facebook().MarkNeedsReconnect(r.Context(), userID); err != nil && !errors.Is(err, store.ErrNotFound) {
		api.Fail(w, r, err)
		return
	}
	httpx.Error(w, http.StatusConflict, MsgReconnect)
}

// graphFailed answers a Graph failure that is not about the token.
func graphFailed(w http.ResponseWriter, r *http.Request, err error) {
	log.Warn().Err(err).Str("request_id", httpx.RequestID(r)).Msg("facebook graph call failed")
	if RateLimited(err) {
		httpx.Error(w, http.StatusServiceUnavailable, MsgBusy)
		return
	}
	httpx.Error(w, http.StatusBadGateway, MsgNoAnswer)
}

// importView renders an import for the viewer.
func (h *H) importView(ctx context.Context, viewerID string, imp *models.FacebookImport) (contract.FacebookImport, error) {
	friends, err := h.friendsOnApp(ctx, viewerID, imp.FriendFBIDs)
	if err != nil {
		return contract.FacebookImport{}, err
	}
	ratings := contract.Ratings{}
	for k, v := range imp.SuggestedRatings {
		ratings[k] = v
	}
	interests := imp.Interests
	if interests == nil {
		interests = []string{}
	}
	out := contract.FacebookImport{
		ImportedAt:       contract.NewTime(imp.ImportedAt),
		LikedPages:       imp.LikedPages,
		SuggestedRatings: ratings,
		Interests:        interests,
		FriendsOnApp:     friends,
	}
	if imp.City != nil && *imp.City != "" {
		out.HomeArea = imp.City
	}
	return out, nil
}

// friendsOnApp turns the Facebook friends an import found into the
// SideQuests people linked to them, with their current relation to the
// viewer, sorted by name.
func (h *H) friendsOnApp(ctx context.Context, viewerID string, fbIDs []string) ([]contract.UserSearchResult, error) {
	out := []contract.UserSearchResult{}
	if len(fbIDs) == 0 {
		return out, nil
	}
	linked, err := h.d.Store.Facebook().UserIDsByFBUserID(ctx, fbIDs)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(linked))
	for _, id := range linked {
		if id != viewerID && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	users, err := h.d.Store.Users().ByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	friends, pending, err := h.d.Store.Facebook().FriendLinks(ctx, viewerID, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		u := users[id]
		if u == nil {
			continue
		}
		row := contract.UserSearchResult{Person: view.PersonRef(u, h.d.Cfg.PublicBaseURL), Relation: contract.RelationNone}
		if friends[id] {
			row.Relation = contract.RelationFriend
		} else if req, ok := pending[id]; ok {
			row.Relation = contract.RelationOutgoing
			if req.ToID == viewerID {
				row.Relation = contract.RelationIncoming
			}
			requestID := req.ID
			row.RequestID = &requestID
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Person.Name), strings.ToLower(out[j].Person.Name)
		if a != b {
			return a < b
		}
		return out[i].Person.ID < out[j].Person.ID
	})
	return out, nil
}

// splitScopes sorts what /me/permissions reported: granted scopes, and the
// optional scopes that are not granted (declined, expired or missing).
func splitScopes(perms []Permission) (granted, declined []string) {
	status := map[string]string{}
	for _, p := range perms {
		status[p.Name] = p.Status
	}
	granted, declined = []string{}, []string{}
	for _, scope := range requestedScopes {
		if status[scope] == "granted" {
			granted = append(granted, scope)
		} else if slices.Contains(optionalScopes, scope) {
			declined = append(declined, scope)
		}
	}
	return granted, declined
}

// moveToDeclined records a scope Graph refused although it was listed as granted.
func moveToDeclined(granted, declined []string, scope string) ([]string, []string) {
	granted = slices.DeleteFunc(slices.Clone(granted), func(s string) bool { return s == scope })
	if !slices.Contains(declined, scope) {
		declined = append(slices.Clone(declined), scope)
	}
	return granted, knownScopes(declined)
}

// knownScopes keeps the optional scopes, in the dialog's order.
func knownScopes(scopes []string) []string {
	out := []string{}
	for _, scope := range optionalScopes {
		if slices.Contains(scopes, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// ---- DELETE /integrations/facebook ----------------------------------------

// Disconnect is DELETE /integrations/facebook → 204: SideQuests is removed
// on Facebook (best effort), then the connection, the import and the
// interests are deleted. Saved preferences stay. Repeating it is harmless.
func (h *H) Disconnect(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx, userID := r.Context(), user.ID.Hex()
	fbs := h.d.Store.Facebook()
	acct, err := fbs.Account(ctx, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		api.Fail(w, r, err)
		return
	}
	if acct != nil && acct.AccessTokenEnc != "" {
		h.revoke(ctx, userID, acct.AccessTokenEnc)
	}
	if err := fbs.Forget(ctx, userID); err != nil {
		api.Fail(w, r, err)
		return
	}
	if acct != nil || len(user.FacebookInterests) > 0 {
		h.d.RefreshProfileAsync(userID, asyncProfileTimeout)
	}
	httpx.NoContent(w)
}

// revoke removes SideQuests on Facebook; failures are logged and ignored,
// since the person asked for the connection to go either way.
func (h *H) revoke(ctx context.Context, userID, sealed string) {
	g := h.graph()
	if g == nil || h.box == nil {
		return
	}
	token, err := h.box.open(userID, sealed)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, revokeBudget)
	defer cancel()
	if err := g.Revoke(ctx, token); err != nil {
		log.Warn().Err(err).Str("user", userID).Msg("facebook: revoke failed; deleting the connection anyway")
	}
}

// ---- Facebook's callbacks (public, signed_request) ------------------------

// Deauthorize is POST /integrations/facebook/deauthorize: the person removed
// SideQuests on Facebook. The token is deleted and the connection reads as
// disconnected; the import stays until they disconnect here or ask Facebook
// for a data deletion.
func (h *H) Deauthorize(w http.ResponseWriter, r *http.Request) {
	req, ok := h.signedRequest(w, r)
	if !ok {
		return
	}
	userID, err := h.d.Store.Facebook().Deauthorize(r.Context(), req.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		api.Fail(w, r, err)
		return
	}
	if userID != "" {
		log.Info().Str("user", userID).Msg("facebook: deauthorized on Facebook, token deleted")
	}
	httpx.JSON(w, http.StatusOK, contract.Empty)
}

// DataDeletion is POST /integrations/facebook/data-deletion: everything
// imported for that Facebook user is deleted, and the answer names a status
// page and a confirmation code (kept 90 days).
func (h *H) DataDeletion(w http.ResponseWriter, r *http.Request) {
	req, ok := h.signedRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	fbs := h.d.Store.Facebook()
	userID := ""
	acct, err := fbs.AccountByFBUserID(ctx, req.UserID)
	switch {
	case err == nil:
		userID = acct.UserID
		if err := fbs.Forget(ctx, userID); err != nil {
			api.Fail(w, r, err)
			return
		}
		h.d.RefreshProfileAsync(userID, asyncProfileTimeout)
		log.Info().Str("user", userID).Msg("facebook: data deleted on Facebook's request")
	case !errors.Is(err, store.ErrNotFound):
		api.Fail(w, r, err)
		return
	}
	code, err := h.d.Store.WebSessions().Insert(ctx, models.WebSession{
		UserID: userID, Purpose: store.PurposeFacebookDelete, Provider: provider,
	}, deletionTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.DataDeletionResponse{
		URL:              h.d.Cfg.PublicBaseURL + "/integrations/facebook/deletion-status?code=" + url.QueryEscape(code),
		ConfirmationCode: code,
	})
}

// signedRequest verifies the form's signed_request with the app secret,
// answering 400 (or 503 without a secret) when it cannot.
func (h *H) signedRequest(w http.ResponseWriter, r *http.Request) (SignedRequest, bool) {
	if h.d.Cfg.FBAppSecret == "" {
		httpx.Error(w, http.StatusServiceUnavailable, MsgNotSetUp)
		return SignedRequest{}, false
	}
	if err := r.ParseForm(); err != nil {
		httpx.Error(w, http.StatusBadRequest, MsgBadSignature)
		return SignedRequest{}, false
	}
	req, err := ParseSignedRequest(r.PostForm.Get("signed_request"), h.d.Cfg.FBAppSecret)
	if err != nil {
		log.Warn().Str("request_id", httpx.RequestID(r)).Str("path", r.URL.Path).Msg("facebook: rejected signed_request")
		httpx.Error(w, http.StatusBadRequest, MsgBadSignature)
		return SignedRequest{}, false
	}
	return req, true
}
