package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// Caps of GET /search.
const (
	searchSidequests = 20
	searchPeople     = 20
	searchPlaces     = 5
)

// Search is GET /search?q= → SearchResults (Home's search bar). An empty q
// finds nothing. sidequests: the viewer's active plans whose title or a
// stop's title contains q; people: as GET /users/search (handle or name
// word starts with q, with the viewer's relation to them); places: catalog
// places near the home base; posts: the forum posts the viewer may see
// that mention q (rendered by the social area; none until UseSocial).
func (h *H) Search(w http.ResponseWriter, r *http.Request) {
	out := contract.SearchResults{
		Sidequests: []contract.Itinerary{},
		People:     []contract.UserSearchResult{},
		Places:     []contract.Place{},
		Posts:      []contract.ForumPost{},
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ctx, uid := r.Context(), user.ID.Hex()

	its, err := h.d.Store.Itineraries().SearchActive(ctx, uid, q, h.d.BusinessNow(ctx), searchSidequests)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if out.Sidequests, err = Views(ctx, h.d, its, uid); err != nil {
		api.Fail(w, r, err)
		return
	}

	if out.People, err = h.searchPeople(r, uid, q); err != nil {
		api.Fail(w, r, err)
		return
	}

	places, err := h.d.Store.Catalog().SearchPlaces(ctx, user.Catalog, q, homePoint(user), searchPlaces)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	for _, p := range places {
		out.Places = append(out.Places, contract.Place{Name: p.Name, Coordinate: &contract.Coordinate{Lat: p.Lat, Lng: p.Lng}})
	}

	if s := currentSocial(); s != nil {
		posts, err := s.SearchPosts(ctx, h.d, user, q, httpx.TZ(r))
		if err != nil {
			log.Warn().Err(err).Str("user", uid).Msg("search: forum posts left out")
		} else if posts != nil {
			out.Posts = posts
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// searchPeople is GET /users/search's result for q: other people whose
// handle or a word of their name starts with q, with how they relate to
// the viewer.
func (h *H) searchPeople(r *http.Request, uid, q string) ([]contract.UserSearchResult, error) {
	ctx := r.Context()
	people, err := h.d.Store.Users().Search(ctx, q, uid, searchPeople)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID.Hex())
	}
	relations, err := h.d.Store.PeopleRelations(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	out := make([]contract.UserSearchResult, 0, len(people))
	for _, p := range people {
		rel := relations[p.ID.Hex()]
		result := contract.UserSearchResult{
			Person:   view.PersonRef(p, h.d.Cfg.PublicBaseURL),
			Relation: contract.FriendRelation(rel.Kind),
		}
		if !result.Relation.Valid() {
			result.Relation = contract.RelationNone
		}
		result.RequestID = view.StrPtr(rel.RequestID)
		out = append(out, result)
	}
	return out, nil
}

// homePoint is the user's home base as a point (nil without one).
func homePoint(u *models.User) *travel.Point {
	if u.HomeBase == nil {
		return nil
	}
	return &travel.Point{Lat: u.HomeBase.Lat, Lng: u.HomeBase.Lng}
}
