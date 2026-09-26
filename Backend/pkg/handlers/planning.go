package handlers

import (
	"Backend/pkg/env"
	"Backend/pkg/middleware"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type SaveItineraryRequest struct {
	OptionID     string                 `json:"option_id,omitempty"`
	Title        string                 `json:"title"`
	Date         string                 `json:"date"`
	StartTime    string                 `json:"start_time"`
	BackByTime   string                 `json:"back_by_time"`
	Visibility   string                 `json:"visibility"` // just_me, friends, open
	LockTime     *time.Time             `json:"lock_time,omitempty"`
	MaxGroupSize int                    `json:"max_group_size"`
	Items        []models.ItineraryItem `json:"items"`
}

type GenerateMoreRequest struct {
	Cursor string `json:"cursor"`
}

func activityAddressString(act models.Activity) string {
	if act.Address != nil && act.Address.Formatted != nil && *act.Address.Formatted != "" {
		return *act.Address.Formatted
	}
	if act.Address != nil && act.Address.Street != nil && *act.Address.Street != "" {
		var parts []string
		parts = append(parts, *act.Address.Street)
		if act.Address.Locality != nil && *act.Address.Locality != "" {
			parts = append(parts, *act.Address.Locality)
		}
		if act.Address.Region != nil && *act.Address.Region != "" {
			parts = append(parts, *act.Address.Region)
		}
		return strings.Join(parts, ", ")
	}
	if act.VenueName != nil && *act.VenueName != "" {
		return *act.VenueName
	}
	if act.City != "" {
		return strings.Title(act.City)
	}
	return ""
}

func calculateDistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180.0
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	d := 6371.0 * c
	if d < 0.1 {
		return 0.5
	}
	return math.Round(d*100) / 100
}

func calculateLegDuration(distKm float64, mode string) int {
	switch strings.ToLower(mode) {
	case "rideshare":
		dur := int(distKm/35.0*60) + 3
		if dur < 4 {
			return 4
		}
		return dur
	case "marta":
		dur := int(distKm/20.0*60) + 5
		if dur < 8 {
			return 8
		}
		return dur
	default: // walk
		dur := int(distKm / 4.5 * 60)
		if dur < 5 {
			return 5
		}
		return dur
	}
}

func activityToPlanStop(act models.Activity, order int) models.PlanStop {
	lat, lng := 0.0, 0.0
	if len(act.Location.Coordinates) >= 2 {
		lng = act.Location.Coordinates[0]
		lat = act.Location.Coordinates[1]
	}

	duration := 60
	if act.Duration != nil && act.Duration.MedianMin > 0 {
		duration = int(act.Duration.MedianMin)
	}

	cost := int64(0)
	if act.Price != nil && act.Price.Cents > 0 {
		cost = act.Price.Cents
	}

	notes := ""
	if act.Summary != nil && *act.Summary != "" {
		notes = *act.Summary
	} else if act.Description != nil && *act.Description != "" {
		notes = *act.Description
	} else if len(act.Tags) > 0 {
		notes = strings.Join(act.Tags, ", ")
	}

	return models.PlanStop{
		ID:                 fmt.Sprintf("stop_%s_%d", act.ID.Hex(), order),
		PlaceID:            act.ID.Hex(),
		Name:               act.Name,
		Address:            activityAddressString(act),
		Lat:                lat,
		Lng:                lng,
		Order:              order,
		DurationMin:        duration,
		EstimatedCostCents: cost,
		Notes:              notes,
	}
}

func activityToItineraryItem(act models.Activity, arriveTime time.Time, duration time.Duration) models.ItineraryItem {
	lat, lng := 0.0, 0.0
	if len(act.Location.Coordinates) >= 2 {
		lng = act.Location.Coordinates[0]
		lat = act.Location.Coordinates[1]
	}

	if act.Duration != nil && act.Duration.MedianMin > 0 {
		duration = time.Duration(act.Duration.MedianMin) * time.Minute
	}

	cost := int64(0)
	if act.Price != nil && act.Price.Cents > 0 {
		cost = act.Price.Cents
	}

	notes := ""
	if act.Summary != nil && *act.Summary != "" {
		notes = *act.Summary
	} else if act.Description != nil && *act.Description != "" {
		notes = *act.Description
	}

	itemType := act.Kind
	if itemType == "" {
		itemType = "activity"
	}

	departTime := arriveTime.Add(duration)

	return models.ItineraryItem{
		ID:           util.GenerateID(),
		Title:        act.Name,
		Type:         itemType,
		LocationName: act.Name,
		Address:      activityAddressString(act),
		Lat:          lat,
		Lng:          lng,
		ArriveTime:   arriveTime,
		DepartTime:   departTime,
		PriceCents:   cost,
		SharedNotes:  notes,
	}
}

func buildPlanOptionsFromActivities(activities []models.Activity, req models.PlanGenerateRequest, numOptions int) []models.PlanOption {
	if len(activities) == 0 {
		return nil
	}

	stopsPerOption := 2
	if len(activities) >= numOptions*3 {
		stopsPerOption = 3
	}

	var options []models.PlanOption
	actIdx := 0

	now := time.Now()
	for optIdx := 0; optIdx < numOptions; optIdx++ {
		if actIdx >= len(activities) {
			break
		}

		var stops []models.PlanStop
		for s := 0; s < stopsPerOption && actIdx < len(activities); s++ {
			stop := activityToPlanStop(activities[actIdx], s)
			stops = append(stops, stop)
			actIdx++
		}

		if len(stops) == 0 {
			break
		}

		var legs []models.PlanLeg
		totalLegDuration := 0
		var routeSummaries []string

		for i := 0; i < len(stops)-1; i++ {
			distKm := 1.5
			if stops[i].Lat != 0 && stops[i+1].Lat != 0 {
				distKm = calculateDistanceKm(stops[i].Lat, stops[i].Lng, stops[i+1].Lat, stops[i+1].Lng)
			}

			mode := "walk"
			if req.RideChoice == "rideshare" || (len(req.TravelModes) > 0 && req.TravelModes[0] == "rideshare") {
				mode = "rideshare"
			} else if distKm > 3.0 {
				mode = "marta"
			} else if len(req.TravelModes) > i && req.TravelModes[i] != "" {
				mode = req.TravelModes[i]
			}

			dur := calculateLegDuration(distKm, mode)
			legs = append(legs, models.PlanLeg{
				FromStopID:  stops[i].ID,
				ToStopID:    stops[i+1].ID,
				Mode:        mode,
				DurationMin: dur,
				DistanceKm:  distKm,
			})
			totalLegDuration += dur
			routeSummaries = append(routeSummaries, fmt.Sprintf("%s %d min", strings.Title(mode), dur))
		}

		var totalCost int64
		var totalStopDuration int
		for _, st := range stops {
			totalCost += st.EstimatedCostCents
			totalStopDuration += st.DurationMin
		}

		totalDuration := totalStopDuration + totalLegDuration
		arrivalTime := now.Add(time.Duration(totalDuration) * time.Minute)
		lateFlag := totalDuration > 240

		var title, summary string
		if len(stops) >= 2 {
			title = fmt.Sprintf("%s & %s Quest", stops[0].Name, stops[1].Name)
			summary = fmt.Sprintf("Explore %s, followed by %s.", stops[0].Name, stops[1].Name)
		} else {
			title = fmt.Sprintf("%s Experience", stops[0].Name)
			summary = fmt.Sprintf("Spend an afternoon at %s.", stops[0].Name)
		}

		routeSummary := strings.Join(routeSummaries, " → ")
		if routeSummary == "" {
			routeSummary = "Direct visit"
		}

		opt := models.PlanOption{
			ID:               util.GenerateID(),
			Title:            title,
			Summary:          summary,
			Stops:            stops,
			Legs:             legs,
			RouteSummary:     routeSummary,
			TotalCostCents:   totalCost,
			TotalDurationMin: totalDuration,
			LateFlag:         lateFlag,
			ArrivalTime:      arrivalTime,
		}

		store.GlobalStore.SavePlanOption(&opt)
		options = append(options, opt)
	}

	return options
}

func GeneratePlans(w http.ResponseWriter, r *http.Request) {
	var req models.PlanGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid plan generation request")
		return
	}

	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}
	if req.StartTime == "" {
		req.StartTime = "18:00"
	}
	if req.BackByTime == "" {
		req.BackByTime = "22:00"
	}

	userAgeBracket := "21_plus"
	var currentUser *models.User
	if claims := middleware.GetUserClaims(r); claims != nil {
		if u, err := store.GlobalStore.GetUserByID(claims.UserID); err == nil {
			currentUser = u
			if u.AgeBracket != nil {
				userAgeBracket = *u.AgeBracket
			}
		}
	}

	// Plan from activities that fit the request's area, window, budget and
	// age, ranked by the ML service, through the itinerary optimizer.
	if planner := env.GetPlanner(); planner != "legacy" {
		dagOptions, dagCursor, reason := planWithOptimizer(r.Context(), req, currentUser, userAgeBracket)
		if len(dagOptions) > 0 || planner == "dag" {
			resp := map[string]interface{}{
				"options":      dagOptions,
				"cursor":       dagCursor,
				"done":         dagCursor == "",
				"planner":      "dag",
				"mood":         req.MoodText,
				"rideshare":    req.RideChoice,
				"travel_modes": req.TravelModes,
			}
			if reason != "" {
				resp["reason"] = reason
			}
			middleware.WriteJSON(w, http.StatusOK, resp)
			return
		}
		log.Info().Str("reason", reason).Msg("itinerary optimizer found nothing; using legacy planner")
	}

	// Legacy planner: first activities by id, ranked, sliced into options.
	activities, nextCursor, hasMore := store.GlobalStore.ListActivities("", req.StartLocation, req.RangeKm, req.Tags, req.BudgetCents, userAgeBracket, "", 15)
	if len(activities) < 4 && len(req.Tags) > 0 {
		activities, nextCursor, hasMore = store.GlobalStore.ListActivities("", req.StartLocation, req.RangeKm, nil, req.BudgetCents, userAgeBracket, "", 15)
	}
	if len(activities) < 3 {
		activities, nextCursor, hasMore = store.GlobalStore.ListActivities("", "", 0, nil, 0, userAgeBracket, "", 15)
	}
	rankedActivities := ml.DefaultClient().RankActivities(
		r.Context(),
		currentUser,
		activities,
		ml.RankingOptions{
			Rerank: true,
		},
		req.MoodText,
		nil,
	)

	optionsList := buildPlanOptionsFromActivities(rankedActivities, req, 3)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"options":      optionsList,
		"cursor":       nextCursor,
		"done":         !hasMore || len(optionsList) < 3,
		"mood":         req.MoodText,
		"rideshare":    req.RideChoice,
		"travel_modes": req.TravelModes,
	})
}

func GenerateMorePlans(w http.ResponseWriter, r *http.Request) {
	var req GenerateMoreRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Pages of an optimizer run come from its saved pool, not a new query.
	if page, next, ok := nextFromPlanPool(req.Cursor, morePageOptions); ok {
		if page == nil {
			page = []models.PlanOption{}
		}
		middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"options": page,
			"cursor":  next,
			"done":    next == "",
			"planner": "dag",
		})
		return
	}

	userAgeBracket := "21_plus"
	var currentUser *models.User
	if claims := middleware.GetUserClaims(r); claims != nil {
		if u, err := store.GlobalStore.GetUserByID(claims.UserID); err == nil {
			currentUser = u
			if u.AgeBracket != nil {
				userAgeBracket = *u.AgeBracket
			}
		}
	}

	// Query next batch of real activities from the database
	activities, nextCursor, hasMore := store.GlobalStore.ListActivities("", "", 0, nil, 0, userAgeBracket, req.Cursor, 10)
	if len(activities) == 0 {
		middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"options": []models.PlanOption{},
			"cursor":  "",
			"done":    true,
		})
		return
	}

	// Rank batch with ML service
	rankedActivities := ml.DefaultClient().RankActivities(
		r.Context(),
		currentUser,
		activities,
		ml.RankingOptions{
			Rerank: true,
		},
		"",
		nil,
	)

	moreOptions := buildPlanOptionsFromActivities(rankedActivities, models.PlanGenerateRequest{}, 2)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"options": moreOptions,
		"cursor":  nextCursor,
		"done":    !hasMore || len(moreOptions) < 2,
	})
}

func RoutePlan(w http.ResponseWriter, r *http.Request) {
	var req models.PlanRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid route request body")
		return
	}

	opt, _ := store.GlobalStore.GetPlanOption(req.OptionID)
	var stops []models.PlanStop
	if opt != nil && len(opt.Stops) > 0 {
		stopMap := make(map[string]models.PlanStop)
		for _, s := range opt.Stops {
			stopMap[s.ID] = s
			stopMap[s.PlaceID] = s
		}
		for i, id := range req.StopOrder {
			if s, ok := stopMap[id]; ok {
				s.Order = i
				stops = append(stops, s)
			}
		}
		if len(stops) == 0 {
			stops = opt.Stops
		}
	}

	if resp, ok := routeDAGPlan(r.Context(), req, stops); ok {
		middleware.WriteJSON(w, http.StatusOK, resp)
		return
	}

	numStops := len(stops)
	if numStops == 0 {
		numStops = len(req.StopOrder)
		if numStops == 0 {
			numStops = 3
		}
	}

	var legs []models.PlanLeg
	totalDuration := 0
	for i := 0; i < numStops-1; i++ {
		from := fmt.Sprintf("stop_%d", i)
		to := fmt.Sprintf("stop_%d", i+1)
		var fromLat, fromLng, toLat, toLng float64
		if i < len(stops) {
			from = stops[i].ID
			fromLat = stops[i].Lat
			fromLng = stops[i].Lng
		}
		if i+1 < len(stops) {
			to = stops[i+1].ID
			toLat = stops[i+1].Lat
			toLng = stops[i+1].Lng
		}

		distKm := 1.5
		if fromLat != 0 && toLat != 0 {
			distKm = calculateDistanceKm(fromLat, fromLng, toLat, toLng)
		}

		mode := "walk"
		if req.Ride == "rideshare" {
			mode = "rideshare"
		} else if len(req.Modes) > i && req.Modes[i] != "" {
			mode = req.Modes[i]
		} else if distKm > 3.0 {
			mode = "marta"
		}

		dur := calculateLegDuration(distKm, mode)
		legs = append(legs, models.PlanLeg{
			FromStopID:  from,
			ToStopID:    to,
			Mode:        mode,
			DurationMin: dur,
			DistanceKm:  distKm,
		})
		stopDur := 45
		if i < len(stops) && stops[i].DurationMin > 0 {
			stopDur = stops[i].DurationMin
		}
		totalDuration += dur + stopDur
	}

	arrival := time.Now().Add(time.Duration(totalDuration) * time.Minute)
	lateFlag := totalDuration > 240

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"option_id":          req.OptionID,
		"recalculated_legs":  legs,
		"total_duration_min": totalDuration,
		"arrival":            arrival,
		"late_flag":          lateFlag,
	})
}

func CreateItinerary(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req SaveItineraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid itinerary request")
		return
	}

	if req.Title == "" {
		req.Title = "SideQuest"
	}
	if req.Visibility == "" {
		req.Visibility = "just_me"
	}
	if req.MaxGroupSize <= 0 {
		req.MaxGroupSize = 6
	}

	now := time.Now().UTC()
	itin := &models.Itinerary{
		ID:           util.GenerateID(),
		HostUserID:   u.ID.Hex(),
		HostName:     u.Name,
		Title:        req.Title,
		Date:         req.Date,
		StartTime:    req.StartTime,
		BackByTime:   req.BackByTime,
		Visibility:   req.Visibility,
		LockTime:     req.LockTime,
		MaxGroupSize: req.MaxGroupSize,
		Members: []models.ItineraryMember{
			{
				UserID:      u.ID.Hex(),
				Name:        u.Name,
				Username:    u.Username,
				PhotoURL:    u.PhotoURL,
				AvatarColor: u.AvatarColor,
				Role:        "host",
			},
		},
		Items:     req.Items,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	userAgeBracket := "21_plus"
	if u.AgeBracket != nil {
		userAgeBracket = *u.AgeBracket
	}

	// If no items provided, retrieve from the selected plan option or directly from database activities
	if len(itin.Items) == 0 {
		if req.OptionID != "" {
			if opt, err := store.GlobalStore.GetPlanOption(req.OptionID); err == nil && len(opt.Stops) > 0 {
				if req.Title == "SideQuest" && opt.Title != "" {
					itin.Title = opt.Title
				}
				curTime := now.Add(1 * time.Hour)
				for i, stop := range opt.Stops {
					itemDur := time.Duration(stop.DurationMin) * time.Minute
					if itemDur <= 0 {
						itemDur = 45 * time.Minute
					}
					// Optimizer stops carry their scheduled times; legacy stops don't.
					arrive := curTime
					if stop.ArriveTime != nil {
						arrive = *stop.ArriveTime
						if stop.DepartTime != nil && stop.DepartTime.After(arrive) {
							itemDur = stop.DepartTime.Sub(arrive)
						}
					}
					itemType := "activity"
					if stop.Kind != "" {
						itemType = stop.Kind
					}
					item := models.ItineraryItem{
						ID:           util.GenerateID(),
						Title:        stop.Name,
						Type:         itemType,
						LocationName: stop.Name,
						Address:      stop.Address,
						Lat:          stop.Lat,
						Lng:          stop.Lng,
						ArriveTime:   arrive,
						DepartTime:   arrive.Add(itemDur),
						PriceCents:   stop.EstimatedCostCents,
						SharedNotes:  stop.Notes,
					}
					if i < len(opt.Legs) {
						leg := opt.Legs[i]
						item.TransitOptions = []models.TransitOption{
							{
								Mode:          leg.Mode,
								DurationMin:   leg.DurationMin,
								DistanceKm:    leg.DistanceKm,
								DepartureTime: arrive.Add(-time.Duration(leg.DurationMin) * time.Minute),
								ArrivalTime:   arrive,
								Summary:       fmt.Sprintf("%s (%.1f km)", strings.Title(leg.Mode), leg.DistanceKm),
							},
						}
					}
					itin.Items = append(itin.Items, item)
					curTime = arrive.Add(itemDur + 15*time.Minute)
				}
			}
		}

		// If still empty, fetch real activities from the database!
		if len(itin.Items) == 0 {
			dbActs, _, _ := store.GlobalStore.ListActivities("", "", 0, nil, 0, userAgeBracket, "", 2)
			curTime := now.Add(1 * time.Hour)
			for _, act := range dbActs {
				item := activityToItineraryItem(act, curTime, 60*time.Minute)
				itin.Items = append(itin.Items, item)
				curTime = item.DepartTime.Add(15 * time.Minute)
			}
		}
	}

	_ = store.GlobalStore.CreateItinerary(itin)

	// Rule of thumb: open plans are published from POST /itineraries
	if itin.Visibility == "open" {
		postLat := 33.7820
		postLng := -84.3680
		if len(itin.Items) > 0 && (itin.Items[0].Lat != 0 || itin.Items[0].Lng != 0) {
			postLat = itin.Items[0].Lat
			postLng = itin.Items[0].Lng
		} else if u.LastLocation != nil && len(u.LastLocation.Coordinates) >= 2 {
			postLng = u.LastLocation.Coordinates[0]
			postLat = u.LastLocation.Coordinates[1]
		}

		forumPost := &models.ForumPost{
			ID:                util.GenerateID(),
			UserID:            u.ID.Hex(),
			AuthorName:        u.Name,
			AuthorUsername:    u.Username,
			AuthorPhoto:       u.PhotoURL,
			Type:              "itinerary",
			Title:             itin.Title,
			Content:           fmt.Sprintf("Open sidequest on %s! Max group size %d. Join us!", itin.Date, itin.MaxGroupSize),
			Lat:               postLat,
			Lng:               postLng,
			Visibility:        "everyone",
			ItineraryID:       itin.ID,
			Tags:              []string{"open_plan", "sidequest"},
			CostCents:         3000,
			OpenOnly:          true,
			JoinRequestsCount: 0,
			CreatedAt:         now,
		}
		store.GlobalStore.CreateForumPost(forumPost)
		realtime.GlobalHub.Broadcast("forum:post_created", forumPost)
	}

	middleware.WriteJSON(w, http.StatusCreated, itin)
}
