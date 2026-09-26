// Package planning is the thin HTTP surface over api.Planner (the planner
// agent owns pkg/planner). Without a planner every route answers 503.
package planning

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"net/http"

	"github.com/gorilla/mux"
)

// MsgWarmingUp is the 503 sentence while no planner is wired.
const MsgWarmingUp = "Planning is warming up. Try again in a moment."

// MsgBadPlan is the 400 for enum values outside the app's choices.
const MsgBadPlan = "Check your start, end and times, then try again."

type H struct{ d *api.Deps }

func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	r.Handle("/plans/generate", d.Protect(h.Generate)).Methods("POST")
	r.Handle("/plans/generate/more", d.Protect(h.More)).Methods("POST")
	r.Handle("/plans/route", d.Protect(h.Route)).Methods("POST")
	r.Handle("/plans/alternatives", d.Protect(h.Alternatives)).Methods("POST")
}

// planner returns the planner or writes the 503.
func (h *H) planner(w http.ResponseWriter) (api.Planner, bool) {
	if h.d.Planner == nil {
		httpx.Error(w, http.StatusServiceUnavailable, MsgWarmingUp)
		return nil, false
	}
	return h.d.Planner, true
}

// Generate is POST /plans/generate (PlanRequest) → PlanBatch.
func (h *H) Generate(w http.ResponseWriter, r *http.Request) {
	p, ok := h.planner(w)
	if !ok {
		return
	}
	var req contract.PlanRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !req.Range.Valid() || !req.Ride.Valid() || !req.Who.Valid() || !req.Pace.Valid() || !req.Modes.Valid() ||
		req.Budget < 0 || req.Budget > 3 {
		httpx.Error(w, http.StatusBadRequest, MsgBadPlan)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	batch, err := p.Generate(r.Context(), user, req, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if batch.Options == nil {
		batch.Options = []contract.PlanOption{}
	}
	httpx.JSON(w, http.StatusOK, batch)
}

// More is POST /plans/generate/more {cursor} → PlanBatch.
func (h *H) More(w http.ResponseWriter, r *http.Request) {
	p, ok := h.planner(w)
	if !ok {
		return
	}
	var req contract.MoreRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	batch, err := p.More(r.Context(), user, req.Cursor)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if batch.Options == nil {
		batch.Options = []contract.PlanOption{}
	}
	httpx.JSON(w, http.StatusOK, batch)
}

// Route is POST /plans/route (RouteRequest) → RouteResult.
func (h *H) Route(w http.ResponseWriter, r *http.Request) {
	p, ok := h.planner(w)
	if !ok {
		return
	}
	var req contract.RouteRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.OptionID == "" || !req.Ride.Valid() || !req.Modes.Valid() {
		httpx.Error(w, http.StatusBadRequest, MsgBadPlan)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	result, err := p.Route(r.Context(), user, req)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if result.Legs == nil {
		result.Legs = []contract.Leg{}
	}
	if result.StopTimes == nil {
		result.StopTimes = []contract.StopWindow{}
	}
	httpx.JSON(w, http.StatusOK, result)
}

// Alternatives is POST /plans/alternatives → [PlanAlternative].
func (h *H) Alternatives(w http.ResponseWriter, r *http.Request) {
	p, ok := h.planner(w)
	if !ok {
		return
	}
	var req contract.AlternativesRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.OptionID == "" || req.StopID == "" {
		httpx.Error(w, http.StatusBadRequest, MsgBadPlan)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	alts, err := p.Alternatives(r.Context(), user, req)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if alts == nil {
		alts = []contract.PlanAlternative{}
	}
	httpx.JSON(w, http.StatusOK, alts)
}
