package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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

	// Generate first 3 curated options
	optionsList := []models.PlanOption{
		{
			ID:      util.GenerateID(),
			Title:   "BeltLine Art & Bites Expedition",
			Summary: "Mural walk along Eastside Trail, dinner at Ponce City Market, and gelato at Krog Street.",
			Stops: []models.PlanStop{
				{
					ID:                 "stop_1",
					PlaceID:            "place_4",
					Name:               "Eastside BeltLine Trail",
					Address:            "10th St NE & Monroe Dr NE, Atlanta, GA",
					Lat:                33.7820,
					Lng:                -84.3680,
					Order:              0,
					DurationMin:        45,
					EstimatedCostCents: 0,
					Notes:              "Scenic walk past Tiny Doors and vibrant murals",
				},
				{
					ID:                 "stop_2",
					PlaceID:            "place_2",
					Name:               "Ponce City Market Food Hall",
					Address:            "675 Ponce De Leon Ave NE, Atlanta, GA",
					Lat:                33.7724,
					Lng:                -84.3656,
					Order:              1,
					DurationMin:        75,
					EstimatedCostCents: 2400,
					Notes:              "Dinner & drinks at the market",
				},
				{
					ID:                 "stop_3",
					PlaceID:            "place_7",
					Name:               "Krog Street Market Desserts",
					Address:            "99 Krog St NE, Atlanta, GA",
					Lat:                33.7582,
					Lng:                -84.3642,
					Order:              2,
					DurationMin:        40,
					EstimatedCostCents: 900,
					Notes:              "Jeni's Splendid Ice Creams stop",
				},
			},
			Legs: []models.PlanLeg{
				{FromStopID: "stop_1", ToStopID: "stop_2", Mode: "walk", DurationMin: 15, DistanceKm: 1.2},
				{FromStopID: "stop_2", ToStopID: "stop_3", Mode: "marta", DurationMin: 18, DistanceKm: 2.1},
			},
			RouteSummary:     "Walk 15 min → MARTA 18 min",
			TotalCostCents:   3300,
			TotalDurationMin: 193,
			LateFlag:         false,
			ArrivalTime:      time.Now().Add(3 * time.Hour),
		},
		{
			ID:      util.GenerateID(),
			Title:   "Midtown Culture & Rooftop Nightlife",
			Summary: "Explore High Museum jazz, stroll to Fox Theatre, followed by rooftop drinks.",
			Stops: []models.PlanStop{
				{
					ID:                 "stop_mid_1",
					PlaceID:            "place_3",
					Name:               "High Museum of Art",
					Address:            "1280 Peachtree St NE, Atlanta, GA",
					Lat:                33.7904,
					Lng:                -84.3853,
					Order:              0,
					DurationMin:        90,
					EstimatedCostCents: 2500,
					Notes:              "Friday Jazz session",
				},
				{
					ID:                 "stop_mid_2",
					PlaceID:            "place_6",
					Name:               "The Fox Theatre District",
					Address:            "660 Peachtree St NE, Atlanta, GA",
					Lat:                33.7725,
					Lng:                -84.3858,
					Order:              1,
					DurationMin:        60,
					EstimatedCostCents: 1500,
					Notes:              "Lounge & drinks",
				},
			},
			Legs: []models.PlanLeg{
				{FromStopID: "stop_mid_1", ToStopID: "stop_mid_2", Mode: "marta", DurationMin: 10, DistanceKm: 1.8},
			},
			RouteSummary:     "MARTA Red/Gold Line 10 min",
			TotalCostCents:   4000,
			TotalDurationMin: 160,
			LateFlag:         false,
			ArrivalTime:      time.Now().Add(2*time.Hour + 40*time.Minute),
		},
		{
			ID:      util.GenerateID(),
			Title:   "Park Haven & Sunset Picnic",
			Summary: "Piedmont Park stroll, skyline sunset over Clara Meer, and evening tacos.",
			Stops: []models.PlanStop{
				{
					ID:                 "stop_park_1",
					PlaceID:            "place_1",
					Name:               "Piedmont Park Meadow",
					Address:            "1320 Monroe Dr NE, Atlanta, GA",
					Lat:                33.7879,
					Lng:                -84.3733,
					Order:              0,
					DurationMin:        60,
					EstimatedCostCents: 0,
					Notes:              "Sunset viewing with blankets",
				},
				{
					ID:                 "stop_park_2",
					PlaceID:            "place_2",
					Name:               "Ponce City Market Rooftop",
					Address:            "675 Ponce De Leon Ave NE, Atlanta, GA",
					Lat:                33.7724,
					Lng:                -84.3656,
					Order:              1,
					DurationMin:        80,
					EstimatedCostCents: 2200,
					Notes:              "Mini-golf and cocktails",
				},
			},
			Legs: []models.PlanLeg{
				{FromStopID: "stop_park_1", ToStopID: "stop_park_2", Mode: "walk", DurationMin: 12, DistanceKm: 0.9},
			},
			RouteSummary:     "Walk 12 min",
			TotalCostCents:   2200,
			TotalDurationMin: 152,
			LateFlag:         false,
			ArrivalTime:      time.Now().Add(2*time.Hour + 32*time.Minute),
		},
	}

	nextCursor := util.EncodeCursor("plans_cursor_page_2")

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"options":      optionsList,
		"cursor":       nextCursor,
		"done":         false,
		"mood":         req.MoodText,
		"rideshare":    req.RideChoice,
		"travel_modes": req.TravelModes,
	})
}

func GenerateMorePlans(w http.ResponseWriter, r *http.Request) {
	var req GenerateMoreRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	moreOptions := []models.PlanOption{
		{
			ID:      util.GenerateID(),
			Title:   "Aquarium Wonders & Downtown Bites",
			Summary: "Georgia aquarium evening pass, Centennial Park stroll, and downtown rooftop dessert.",
			Stops: []models.PlanStop{
				{
					ID:                 "stop_aqua_1",
					PlaceID:            "place_5",
					Name:               "Georgia Aquarium",
					Address:            "225 Baker St NW, Atlanta, GA",
					Lat:                33.7634,
					Lng:                -84.3951,
					Order:              0,
					DurationMin:        100,
					EstimatedCostCents: 4500,
					Notes:              "Ocean Voyager whale shark gallery",
				},
			},
			Legs:             []models.PlanLeg{},
			RouteSummary:     "Walk 8 min",
			TotalCostCents:   4500,
			TotalDurationMin: 108,
			LateFlag:         false,
			ArrivalTime:      time.Now().Add(2 * time.Hour),
		},
		{
			ID:      util.GenerateID(),
			Title:   "Hidden Murals & Speakeasy Tour",
			Summary: "Off-the-beaten-path street art followed by a secret entrance cocktail lounge.",
			Stops: []models.PlanStop{
				{
					ID:                 "stop_mural_1",
					PlaceID:            "place_7",
					Name:               "Krog Street Tunnel Art",
					Address:            "1 Krog St NE, Atlanta, GA",
					Lat:                33.7538,
					Lng:                -84.3644,
					Order:              0,
					DurationMin:        45,
					EstimatedCostCents: 0,
					Notes:              "Graffiti photography",
				},
			},
			Legs:             []models.PlanLeg{},
			RouteSummary:     "Walk 5 min",
			TotalCostCents:   1800,
			TotalDurationMin: 90,
			LateFlag:         false,
			ArrivalTime:      time.Now().Add(1*time.Hour + 30*time.Minute),
		},
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"options": moreOptions,
		"cursor":  "",
		"done":    true, // Out of further options
	})
}

func RoutePlan(w http.ResponseWriter, r *http.Request) {
	var req models.PlanRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid route request body")
		return
	}

	// Recalculate legs, stop times, arrival, late flag after drag-to-reorder
	numStops := len(req.StopOrder)
	if numStops == 0 {
		numStops = 3
	}

	legDuration := 12
	if req.Ride == "rideshare" {
		legDuration = 8
	}

	var legs []models.PlanLeg
	totalDuration := 0
	for i := 0; i < numStops-1; i++ {
		from := fmt.Sprintf("stop_%d", i)
		if i < len(req.StopOrder) {
			from = req.StopOrder[i]
		}
		to := fmt.Sprintf("stop_%d", i+1)
		if i+1 < len(req.StopOrder) {
			to = req.StopOrder[i+1]
		}

		mode := "walk"
		if len(req.Modes) > i {
			mode = req.Modes[i]
		}

		legs = append(legs, models.PlanLeg{
			FromStopID:  from,
			ToStopID:    to,
			Mode:        mode,
			DurationMin: legDuration,
			DistanceKm:  1.5,
		})
		totalDuration += legDuration + 45 // 45 min per stop
	}

	arrival := time.Now().Add(time.Duration(totalDuration) * time.Minute)
	lateFlag := totalDuration > 240 // If over 4 hours, mark late flag

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
		req.Title = "SideQuest Atlanta"
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

	// If no items provided, generate default demo stops
	if len(itin.Items) == 0 {
		itin.Items = []models.ItineraryItem{
			{
				ID:           util.GenerateID(),
				Title:        "BeltLine Gathering",
				Type:         "activity",
				LocationName: "Eastside BeltLine Trail",
				Address:      "10th St NE & Monroe Dr NE, Atlanta, GA",
				Lat:          33.7820,
				Lng:          -84.3680,
				ArriveTime:   now.Add(1 * time.Hour),
				DepartTime:   now.Add(2 * time.Hour),
				PriceCents:   0,
				SharedNotes:  "Meet at the 10th street entrance",
				TransitOptions: []models.TransitOption{
					{
						Mode:          "walk",
						DurationMin:   12,
						DistanceKm:    1.0,
						CostCents:     0,
						Summary:       "Walk via 10th St NE",
						DepartureTime: now.Add(48 * time.Minute),
						ArrivalTime:   now.Add(1 * time.Hour),
					},
					{
						Mode:          "marta",
						DurationMin:   15,
						DistanceKm:    2.4,
						CostCents:     250,
						Summary:       "MARTA Bus 36 from Midtown Station",
						DepartureTime: now.Add(45 * time.Minute),
						ArrivalTime:   now.Add(1 * time.Hour),
					},
					{
						Mode:          "rideshare",
						DurationMin:   7,
						DistanceKm:    2.0,
						CostCents:     950,
						Summary:       "UberX (~4 min pickup)",
						DepartureTime: now.Add(53 * time.Minute),
						ArrivalTime:   now.Add(1 * time.Hour),
					},
				},
			},
			{
				ID:           util.GenerateID(),
				Title:        "Ponce City Market Food & Rooftop",
				Type:         "dining",
				LocationName: "Ponce City Market",
				Address:      "675 Ponce De Leon Ave NE, Atlanta, GA",
				Lat:          33.7724,
				Lng:          -84.3656,
				ArriveTime:   now.Add(2*time.Hour + 15*time.Minute),
				DepartTime:   now.Add(4 * time.Hour),
				PriceCents:   2500,
				SharedNotes:  "Rooftop reservations at 8:30pm",
			},
		}
	}

	_ = store.GlobalStore.CreateItinerary(itin)

	// Rule of thumb: open plans are published from POST /itineraries
	if itin.Visibility == "open" {
		forumPost := &models.ForumPost{
			ID:                util.GenerateID(),
			UserID:            u.ID.Hex(),
			AuthorName:        u.Name,
			AuthorUsername:    u.Username,
			AuthorPhoto:       u.PhotoURL,
			Type:              "itinerary",
			Title:             itin.Title,
			Content:           fmt.Sprintf("Open sidequest on %s! Max group size %d. Join us!", itin.Date, itin.MaxGroupSize),
			Lat:               33.7820,
			Lng:               -84.3680,
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
