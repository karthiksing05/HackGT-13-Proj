package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"net/http"
	"time"
)

// Fares: walking is free, a MARTA ride is $2.50, a rideshare fare is
// unknown (the app shows "[fare]").
const martaFareCents = 250

// TransitOptions is GET /itineraries/{id}/items/{itemId}/transit and GET
// /events/{id}/transit → [TransitOption]: walk, MARTA and rideshare from the
// previous non-transit block's place (or the plan's start) to this block
// (for a transit leg, to the next block or the plan's end).
func (h *H) TransitOptions(w http.ResponseWriter, r *http.Request) {
	it, i, err := h.routeItem(r, api.UserID(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	from, to := legEnds(it, i)
	httpx.JSON(w, http.StatusOK, transitOptions(from, to))
}

// SelectTransit is PUT …/transit ({mode}) → 204: the viewer's "Getting
// there" choice, returned as the item's transit_mode.
func (h *H) SelectTransit(w http.ResponseWriter, r *http.Request) {
	var req contract.TransitSelection
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !req.Mode.Valid() {
		httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
		return
	}
	uid := api.UserID(r)
	it, i, err := h.routeItem(r, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.ItemStates().SetTransitMode(r.Context(), uid, it.Items[i].ID, it.ID, string(req.Mode)); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// legEnds is where getting to item i starts and ends (busy blocks are not
// places the plan goes).
func legEnds(it *models.Itinerary, i int) (from, to *models.PlaceDoc) {
	from = &it.StartPlace
	for j := i - 1; j >= 0; j-- {
		if it.Items[j].Kind != models.ItemTransit && it.Items[j].Kind != kindBusy {
			from = it.Items[j].Place
			break
		}
	}
	if it.Items[i].Kind != models.ItemTransit {
		return from, it.Items[i].Place
	}
	to = &it.EndPlace
	for j := i + 1; j < len(it.Items); j++ {
		if it.Items[j].Kind != models.ItemTransit && it.Items[j].Kind != kindBusy {
			to = it.Items[j].Place
			break
		}
	}
	return from, to
}

// transitOptions estimates the three ways there (travel.Estimate); without
// both coordinates they are the mock's 18 / 12 / 8 minutes.
func transitOptions(from, to *models.PlaceDoc) []contract.TransitOption {
	free, fare := 0, martaFareCents
	walk, marta, ride := 18, 12, 8
	if from != nil && to != nil && from.HasCoordinate() && to.HasCoordinate() {
		a := travel.Point{Lat: *from.Lat, Lng: *from.Lng}
		b := travel.Point{Lat: *to.Lat, Lng: *to.Lng}
		walk = int(travel.Estimate(a, b, travel.Walk).Duration / time.Minute)
		marta = int(travel.Estimate(a, b, travel.Transit).Duration / time.Minute)
		ride = int(travel.Estimate(a, b, travel.Drive).Duration / time.Minute)
	}
	return []contract.TransitOption{
		{Mode: contract.ModeWalk, Minutes: walk, CostCents: &free},
		{Mode: contract.ModeMarta, Minutes: marta, CostCents: &fare},
		{Mode: contract.ModeRideshare, Minutes: ride},
	}
}
