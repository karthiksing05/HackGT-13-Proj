package store

import (
	"Backend/pkg/datastore"
	"Backend/pkg/models"
	"Backend/pkg/util"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Store provides data access layer for SideQuestz
type Store struct {
	mu             sync.RWMutex
	users          map[string]*models.User
	resets         map[string]*models.PasswordResetRecord
	places         []models.Place
	events         []models.Event
	itineraries    map[string]*models.Itinerary
	ratings        map[string]*models.Rating
	checkouts      map[string]*models.CheckoutIntent
	posts          map[string]*models.ForumPost
	joinReqs       map[string]*models.JoinRequest
	threads        map[string]*models.Thread
	messages       map[string][]*models.Message
	photos         map[string][]*models.GroupPhoto
	expenses       map[string][]*models.Expense
	requests       map[string]*models.FriendRequest
	invites        map[string]*models.InviteLink
	calendarTokens map[string]string // token -> userID
	userCalTokens  map[string]string // userID -> token
}

var GlobalStore = NewStore()

func NewStore() *Store {
	s := &Store{
		users:          make(map[string]*models.User),
		resets:         make(map[string]*models.PasswordResetRecord),
		itineraries:    make(map[string]*models.Itinerary),
		ratings:        make(map[string]*models.Rating),
		checkouts:      make(map[string]*models.CheckoutIntent),
		posts:          make(map[string]*models.ForumPost),
		joinReqs:       make(map[string]*models.JoinRequest),
		threads:        make(map[string]*models.Thread),
		messages:       make(map[string][]*models.Message),
		photos:         make(map[string][]*models.GroupPhoto),
		expenses:       make(map[string][]*models.Expense),
		requests:       make(map[string]*models.FriendRequest),
		invites:        make(map[string]*models.InviteLink),
		calendarTokens: make(map[string]string),
		userCalTokens:  make(map[string]string),
	}
	s.seedDefaultCatalog()
	return s
}

// CalculateAgeBracket calculates "13_17" | "18_20" | "21_plus" from birthDate
func CalculateAgeBracket(birthDate *time.Time) *string {
	if birthDate == nil {
		res := "21_plus"
		return &res
	}
	now := time.Now()
	age := now.Year() - birthDate.Year()
	if now.YearDay() < birthDate.YearDay() {
		age--
	}
	var res string
	if age < 18 {
		res = "13_17"
	} else if age <= 20 {
		res = "18_20"
	} else {
		res = "21_plus"
	}
	return &res
}

func ptrString(s string) *string {
	return &s
}

func ptrInt(i int) *int {
	return &i
}

func ptrFloat(f float64) *float64 {
	return &f
}

// ---------------- USER OPERATIONS ----------------

func (s *Store) CreateUser(email, passwordHash, name string) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	objID := bson.NewObjectID()
	rawUsername := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	if len(rawUsername) > 15 {
		rawUsername = rawUsername[:15]
	}
	username := fmt.Sprintf("%s_%s", rawUsername, objID.Hex()[:4])

	now := time.Now().UTC()
	ageBracket := "21_plus"
	company := "small_group"
	pace := "balanced"
	spendTier := 1

	user := &models.User{
		ID:           objID,
		Email:        strings.ToLower(email),
		PasswordHash: passwordHash,
		Name:         name,
		Username:     &username,
		AvatarColor:  "ink", // "ink" | "sage" | "clay" | "forest" | "sand"
		AgeBracket:   &ageBracket,
		Status:       "open", // "open" | "online" | "not_free"
		Prefs: models.UserPrefs{
			Ratings: map[string]int{
				"outdoors_parks": 4,
				"live_music":     5,
			},
			Company:    &company,
			Pace:       &pace,
			SpendTier:  &spendTier,
			Flexible:   true,
			SplitStyle: "equal",
			PreferFree: true,
			Answers: models.UserAnswers{
				PerfectAfternoon: "Strolling the BeltLine, grabbing iced matcha, exploring galleries",
				NeverWant:        "Loud cramped venues with huge covers",
				PlanAround:       "Good food and walkable neighborhoods",
			},
		},
		Taste: models.UserTaste{
			Tags: map[string]float64{
				"outdoors": 0.8,
				"food":     0.6,
				"music":    0.7,
			},
			AvoidTags:   []string{"crowded"},
			RatingCount: 0,
		},
		Calendars:    []models.CalendarConn{},
		FriendIDs:    []bson.ObjectID{},
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}

	if datastore.IsConnected() {
		col := datastore.GetCollection("users")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			var existing models.User
			err := col.FindOne(ctx, bson.M{"email": user.Email}).Decode(&existing)
			if err == nil {
				return nil, errors.New("user with this email already exists")
			}
			_, _ = col.InsertOne(ctx, user)
		}
	}

	s.users[user.ID.Hex()] = user
	return user, nil
}

func (s *Store) GetUserByID(idStr string) (*models.User, error) {
	objID, err := bson.ObjectIDFromHex(idStr)
	if err == nil && datastore.IsConnected() {
		col := datastore.GetCollection("users")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			var u models.User
			if err := col.FindOne(ctx, bson.M{"_id": objID}).Decode(&u); err == nil {
				return &u, nil
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.users[idStr]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	normEmail := strings.ToLower(email)
	if datastore.IsConnected() {
		col := datastore.GetCollection("users")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			var u models.User
			if err := col.FindOne(ctx, bson.M{"email": normEmail}).Decode(&u); err == nil {
				return &u, nil
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if strings.ToLower(u.Email) == normEmail {
			return u, nil
		}
	}
	return nil, errors.New("user not found")
}

func (s *Store) UpdateUser(u *models.User) error {
	u.UpdatedAt = time.Now().UTC()
	u.LastActiveAt = time.Now().UTC()
	if datastore.IsConnected() {
		col := datastore.GetCollection("users")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			_, _ = col.ReplaceOne(ctx, bson.M{"_id": u.ID}, u)
		}
	}

	s.mu.Lock()
	s.users[u.ID.Hex()] = u
	s.mu.Unlock()
	return nil
}

func (s *Store) SearchUsers(q string, limit int) []*models.UserSummary {
	q = strings.ToLower(strings.TrimPrefix(q, "@"))
	var results []*models.UserSummary

	if datastore.IsConnected() {
		col := datastore.GetCollection("users")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			filter := bson.M{
				"$or": []bson.M{
					{"name": bson.M{"$regex": q, "$options": "i"}},
					{"username": bson.M{"$regex": q, "$options": "i"}},
				},
			}
			cur, err := col.Find(ctx, filter)
			if err == nil {
				var dbUsers []models.User
				_ = cur.All(ctx, &dbUsers)
				for _, u := range dbUsers {
					results = append(results, &models.UserSummary{
						ID:          u.ID.Hex(),
						Name:        u.Name,
						Username:    u.Username,
						PhotoURL:    u.PhotoURL,
						Status:      u.Status,
						AvatarColor: u.AvatarColor,
					})
					if len(results) >= limit {
						return results
					}
				}
				if len(results) > 0 {
					return results
				}
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		uname := ""
		if u.Username != nil {
			uname = *u.Username
		}
		if strings.Contains(strings.ToLower(u.Name), q) || strings.Contains(strings.ToLower(uname), q) {
			results = append(results, &models.UserSummary{
				ID:          u.ID.Hex(),
				Name:        u.Name,
				Username:    u.Username,
				PhotoURL:    u.PhotoURL,
				Status:      u.Status,
				AvatarColor: u.AvatarColor,
			})
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

// ---------------- PASSWORD RESET ----------------

func (s *Store) CreatePasswordReset(email string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	code := util.GenerateSixDigitCode()
	record := &models.PasswordResetRecord{
		ID:        util.GenerateID(),
		Email:     strings.ToLower(email),
		Code:      code,
		Verified:  false,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	s.resets[record.Email] = record
	return code
}

func (s *Store) VerifyPasswordReset(email, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.resets[strings.ToLower(email)]
	if !ok || record.ExpiresAt.Before(time.Now()) || record.Code != code {
		return "", errors.New("invalid or expired verification code")
	}
	record.Verified = true

	resetToken, err := util.GenerateResetToken(email)
	if err != nil {
		return "", err
	}
	return resetToken, nil
}

func (s *Store) ResetPassword(email, newPasswordHash string) error {
	u, err := s.GetUserByEmail(email)
	if err != nil {
		return err
	}
	u.PasswordHash = newPasswordHash
	return s.UpdateUser(u)
}

// ---------------- INTEGRATIONS & PAYMENTS ----------------

func (s *Store) GetUserCalendars(userID string) []models.CalendarConn {
	u, err := s.GetUserByID(userID)
	if err != nil || u.Calendars == nil {
		return []models.CalendarConn{}
	}
	return u.Calendars
}

func (s *Store) SetUserCalendar(userID, provider, tokenRef string, connected bool) {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return
	}
	var updated []models.CalendarConn
	for _, c := range u.Calendars {
		if c.Provider != provider {
			updated = append(updated, c)
		}
	}
	if connected {
		updated = append(updated, models.CalendarConn{
			Provider: provider,
			TokenRef: tokenRef,
			SyncedAt: time.Now().UTC(),
		})
	}
	u.Calendars = updated
	_ = s.UpdateUser(u)
}

func (s *Store) SetUserCard(userID string, card *models.UserCard) error {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}
	u.Card = card
	return s.UpdateUser(u)
}

func (s *Store) GetUserCard(userID string) *models.UserCard {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return nil
	}
	return u.Card
}

// ---------------- CALENDAR FEED & TOKENS ----------------

type calendarTokenDoc struct {
	Token     string    `bson:"token"`
	UserID    string    `bson:"userId"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

func (s *Store) GetOrCreateCalendarToken(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tok, ok := s.userCalTokens[userID]; ok {
		return tok
	}

	if datastore.IsConnected() {
		col := datastore.GetCollection("calendar_tokens")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			var doc calendarTokenDoc
			err := col.FindOne(ctx, bson.M{"userId": userID}).Decode(&doc)
			cancel()
			if err == nil && doc.Token != "" {
				s.calendarTokens[doc.Token] = userID
				s.userCalTokens[userID] = doc.Token
				return doc.Token
			}
		}
	}

	tok := strings.ReplaceAll(util.GenerateID(), "-", "")
	s.calendarTokens[tok] = userID
	s.userCalTokens[userID] = tok

	if datastore.IsConnected() {
		col := datastore.GetCollection("calendar_tokens")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			_, _ = col.DeleteMany(ctx, bson.M{"userId": userID})
			_, _ = col.InsertOne(ctx, calendarTokenDoc{
				Token:     tok,
				UserID:    userID,
				UpdatedAt: time.Now().UTC(),
			})
			cancel()
		}
	}

	return tok
}

func (s *Store) RegenerateCalendarToken(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldTok, ok := s.userCalTokens[userID]; ok {
		delete(s.calendarTokens, oldTok)
	}

	newTok := strings.ReplaceAll(util.GenerateID(), "-", "")
	s.calendarTokens[newTok] = userID
	s.userCalTokens[userID] = newTok

	if datastore.IsConnected() {
		col := datastore.GetCollection("calendar_tokens")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			_, _ = col.DeleteMany(ctx, bson.M{"userId": userID})
			_, _ = col.InsertOne(ctx, calendarTokenDoc{
				Token:     newTok,
				UserID:    userID,
				UpdatedAt: time.Now().UTC(),
			})
			cancel()
		}
	}

	return newTok
}

func (s *Store) GetUserIDByCalendarToken(token string) (string, error) {
	s.mu.RLock()
	userID, ok := s.calendarTokens[token]
	s.mu.RUnlock()
	if ok {
		return userID, nil
	}

	if datastore.IsConnected() {
		col := datastore.GetCollection("calendar_tokens")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			var doc calendarTokenDoc
			err := col.FindOne(ctx, bson.M{"token": token}).Decode(&doc)
			cancel()
			if err == nil && doc.UserID != "" {
				s.mu.Lock()
				s.calendarTokens[doc.Token] = doc.UserID
				s.userCalTokens[doc.UserID] = doc.Token
				s.mu.Unlock()
				return doc.UserID, nil
			}
		}
	}

	return "", errors.New("calendar feed token not found")
}

func (s *Store) GenerateUserICS(userID string) (string, error) {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return "", err
	}

	itins, _, _ := s.ListActiveItineraries(userID, "", 100)

	var events []util.ICSEvent
	now := time.Now()

	for _, it := range itins {
		startDate := now
		if parsed, err := time.Parse("2006-01-02", it.Date); err == nil {
			startDate = parsed
		}

		startTime := startDate
		if it.StartTime != "" {
			if parsedT, err := time.Parse("15:04", it.StartTime); err == nil {
				startTime = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), parsedT.Hour(), parsedT.Minute(), 0, 0, startDate.Location())
			}
		} else {
			// default start time 11:00 AM on scheduled date
			startTime = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 11, 0, 0, 0, startDate.Location())
		}

		endTime := startTime.Add(3 * time.Hour)
		if it.BackByTime != "" {
			if parsedT, err := time.Parse("15:04", it.BackByTime); err == nil {
				endTime = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), parsedT.Hour(), parsedT.Minute(), 0, 0, startDate.Location())
			}
		}

		var descParts []string
		descParts = append(descParts, fmt.Sprintf("SideQuest: %s", it.Title))
		if it.MaxGroupSize > 0 {
			descParts = append(descParts, fmt.Sprintf("Group size: %d members", len(it.Members)))
		}

		location := "Atlanta, GA"
		if len(it.Items) > 0 {
			location = it.Items[0].LocationName
			if it.Items[0].Address != "" {
				location = fmt.Sprintf("%s, %s", it.Items[0].LocationName, it.Items[0].Address)
			}
			descParts = append(descParts, "\nStops:")
			for idx, item := range it.Items {
				descParts = append(descParts, fmt.Sprintf("%d. %s (%s)", idx+1, item.Title, item.Type))
				if item.SharedNotes != "" {
					descParts = append(descParts, fmt.Sprintf("   Note: %s", item.SharedNotes))
				}
			}
		}

		events = append(events, util.ICSEvent{
			UID:         fmt.Sprintf("itin-%s@sidequestz.app", it.ID),
			Start:       startTime,
			End:         endTime,
			Summary:     fmt.Sprintf("SideQuest: %s", it.Title),
			Description: strings.Join(descParts, "\n"),
			Location:    location,
			Status:      "CONFIRMED",
		})

		for _, item := range it.Items {
			if !item.ArriveTime.IsZero() {
				stopEnd := item.DepartTime
				if stopEnd.IsZero() {
					stopEnd = item.ArriveTime.Add(45 * time.Minute)
				}
				stopLocation := item.LocationName
				if item.Address != "" {
					stopLocation = fmt.Sprintf("%s, %s", item.LocationName, item.Address)
				}
				events = append(events, util.ICSEvent{
					UID:         fmt.Sprintf("stop-%s-%s@sidequestz.app", it.ID, item.ID),
					Start:       item.ArriveTime,
					End:         stopEnd,
					Summary:     fmt.Sprintf("%s — %s", it.Title, item.Title),
					Description: fmt.Sprintf("Stop: %s\nType: %s\nNotes: %s", item.Title, item.Type, item.SharedNotes),
					Location:    stopLocation,
					Status:      "CONFIRMED",
				})
			}
		}
	}

	calName := fmt.Sprintf("SideQuestz - %s", u.Name)
	return util.BuildICSCalendar(calName, events), nil
}

// ---------------- PLACES & EVENTS ----------------

func (s *Store) SearchPlaces(q, near string) []models.Place {
	q = strings.ToLower(q)
	var matches []models.Place
	for _, p := range s.places {
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Category), q) {
			matches = append(matches, p)
		}
	}
	return matches
}

func (s *Store) ReverseGeocode(lat, lng float64) *models.Place {
	var closest models.Place
	minDist := math.MaxFloat64
	for _, p := range s.places {
		d := (p.Lat-lat)*(p.Lat-lat) + (p.Lng-lng)*(p.Lng-lng)
		if d < minDist {
			minDist = d
			closest = p
		}
	}
	if minDist < 0.005 {
		return &closest
	}
	return &models.Place{
		ID:       util.GenerateID(),
		Name:     fmt.Sprintf("Dropped Pin (%.4f, %.4f)", lat, lng),
		Address:  fmt.Sprintf("%.4f, %.4f, Atlanta, GA", lat, lng),
		Lat:      lat,
		Lng:      lng,
		Category: "landmark",
	}
}

func (s *Store) ListEvents(near string, radius float64, tags []string, maxPrice int64, userAgeBracket string, cursor string, limit int) ([]models.Event, string, bool) {
	var filtered []models.Event

	for _, e := range s.events {
		// Age filtering based on event name/tags and user ageBracket ("13_17", "18_20", "21_plus")
		eventNameLower := strings.ToLower(e.Name)
		is21Plus := strings.Contains(eventNameLower, "21+")
		is18Plus := strings.Contains(eventNameLower, "18+") || is21Plus

		if userAgeBracket == "13_17" && is18Plus {
			continue
		}
		if userAgeBracket == "18_20" && is21Plus {
			continue
		}

		if maxPrice > 0 && e.Price != nil && e.Price.Cents > maxPrice {
			continue
		}

		if len(tags) > 0 {
			tagMatched := false
			for _, t := range tags {
				for _, et := range e.Tags {
					if strings.EqualFold(t, et) {
						tagMatched = true
						break
					}
				}
			}
			if !tagMatched {
				continue
			}
		}

		filtered = append(filtered, e)
	}

	startIdx := 0
	if cursor != "" {
		if decoded, err := util.DecodeCursor(cursor); err == nil {
			for i, e := range filtered {
				if e.ID.Hex() == decoded {
					startIdx = i + 1
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	hasMore := false
	if endIdx < len(filtered) {
		hasMore = true
	} else {
		endIdx = len(filtered)
	}

	var page []models.Event
	if startIdx < len(filtered) {
		page = filtered[startIdx:endIdx]
	}

	var nextCursor string
	if hasMore && len(page) > 0 {
		nextCursor = util.EncodeCursor(page[len(page)-1].ID.Hex())
	}

	return page, nextCursor, hasMore
}

func (s *Store) GetEventByID(idStr string) (*models.Event, error) {
	for _, e := range s.events {
		if e.ID.Hex() == idStr {
			return &e, nil
		}
	}
	return nil, errors.New("event not found")
}

// ---------------- ITINERARIES ----------------

func (s *Store) CreateItinerary(itin *models.Itinerary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.itineraries[itin.ID] = itin
	if datastore.IsConnected() {
		col := datastore.GetCollection("itineraries")
		if col != nil {
			ctx, cancel := datastore.GetCtx()
			defer cancel()
			_, _ = col.InsertOne(ctx, itin)
		}
	}
	return nil
}

func (s *Store) GetItinerary(id string) (*models.Itinerary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if it, ok := s.itineraries[id]; ok {
		return it, nil
	}
	return nil, errors.New("itinerary not found")
}

func (s *Store) ListActiveItineraries(userID string, cursor string, limit int) ([]*models.Itinerary, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var active []*models.Itinerary
	for _, it := range s.itineraries {
		if it.Status == "active" {
			active = append(active, it)
		}
	}

	startIdx := 0
	if cursor != "" {
		if decoded, err := util.DecodeCursor(cursor); err == nil {
			for i, item := range active {
				if item.ID == decoded {
					startIdx = i + 1
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	hasMore := false
	if endIdx < len(active) {
		hasMore = true
	} else {
		endIdx = len(active)
	}

	var page []*models.Itinerary
	if startIdx < len(active) {
		page = active[startIdx:endIdx]
	}

	var nextCursor string
	if hasMore && len(page) > 0 {
		nextCursor = util.EncodeCursor(page[len(page)-1].ID)
	}
	return page, nextCursor, hasMore
}

func (s *Store) UpdateItinerary(it *models.Itinerary) error {
	it.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.itineraries[it.ID] = it
	return nil
}

func (s *Store) DeleteItinerary(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.itineraries, id)
	return nil
}

// ---------------- RATINGS & TASTE PROFILE ----------------

func (s *Store) SaveRating(r *models.Rating) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ratings[r.ItemID] = r

	// Update user learned taste (0..1 tag weights and ratingCount)
	if u, ok := s.users[r.UserID]; ok {
		u.Taste.RatingCount++
		if u.Taste.Tags == nil {
			u.Taste.Tags = make(map[string]float64)
		}

		scoreDelta := float64(r.Stars-3) * 0.1 // -0.2 to +0.2
		for _, tag := range r.Tags {
			t := strings.ToLower(tag)
			curr := u.Taste.Tags[t]
			if curr == 0 {
				curr = 0.5
			}
			newScore := math.Max(0.0, math.Min(1.0, curr+scoreDelta))
			u.Taste.Tags[t] = math.Round(newScore*100) / 100
		}
	}
	return nil
}

func (s *Store) ListPastEvents(userID string, unratedOnly bool) []*models.PastEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	events := []*models.PastEvent{
		{
			ItemID:       "item_past_1",
			EventID:      "evt_1",
			UserID:       userID,
			Title:        "High Museum of Art — Jazz Night",
			Date:         now.AddDate(0, 0, -2),
			PhotoURL:     ptrString("https://images.unsplash.com/photo-1518998053901-5348d3961a04"),
			LocationName: "High Museum of Art, Midtown",
		},
		{
			ItemID:       "item_past_2",
			EventID:      "evt_2",
			UserID:       userID,
			Title:        "Ponce City Market Food Tour",
			Date:         now.AddDate(0, 0, -5),
			PhotoURL:     ptrString("https://images.unsplash.com/photo-1555396273-367ea4eb4db5"),
			LocationName: "Ponce City Market",
		},
	}

	var results []*models.PastEvent
	for _, pe := range events {
		if r, ok := s.ratings[pe.ItemID]; ok {
			pe.Rated = true
			pe.Rating = r
		}
		if unratedOnly && pe.Rated {
			continue
		}
		results = append(results, pe)
	}
	return results
}

// ---------------- AGENT CHECKOUT ----------------

func (s *Store) CreateCheckoutIntent(ci *models.CheckoutIntent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkouts[ci.ID] = ci
}

func (s *Store) GetCheckoutIntent(id string) (*models.CheckoutIntent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ci, ok := s.checkouts[id]; ok {
		return ci, nil
	}
	return nil, errors.New("checkout intent not found")
}

func (s *Store) UpdateCheckoutIntent(ci *models.CheckoutIntent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ci.UpdatedAt = time.Now().UTC()
	s.checkouts[ci.ID] = ci
}

// ---------------- FORUM & JOIN REQUESTS ----------------

func (s *Store) ListForumPosts(cursor string, limit int, tags []string, openOnly bool) ([]*models.ForumPost, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var posts []*models.ForumPost
	for _, p := range s.posts {
		if openOnly && !p.OpenOnly {
			continue
		}
		posts = append(posts, p)
	}

	startIdx := 0
	if cursor != "" {
		if decoded, err := util.DecodeCursor(cursor); err == nil {
			for i, p := range posts {
				if p.ID == decoded {
					startIdx = i + 1
					break
				}
			}
		}
	}

	endIdx := startIdx + limit
	hasMore := false
	if endIdx < len(posts) {
		hasMore = true
	} else {
		endIdx = len(posts)
	}

	var page []*models.ForumPost
	if startIdx < len(posts) {
		page = posts[startIdx:endIdx]
	}

	var nextCursor string
	if hasMore && len(page) > 0 {
		nextCursor = util.EncodeCursor(page[len(page)-1].ID)
	}
	return page, nextCursor, hasMore
}

func (s *Store) CreateForumPost(p *models.ForumPost) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.posts[p.ID] = p
}

func (s *Store) DeleteForumPost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.posts, id)
	return nil
}

func (s *Store) CreateJoinRequest(jr *models.JoinRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.joinReqs[jr.ID] = jr
	if p, ok := s.posts[jr.PostID]; ok {
		p.JoinRequestsCount++
	}
}

func (s *Store) DeleteJoinRequest(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if jr, ok := s.joinReqs[id]; ok {
		if p, pok := s.posts[jr.PostID]; pok && p.JoinRequestsCount > 0 {
			p.JoinRequestsCount--
		}
		delete(s.joinReqs, id)
	}
}

func (s *Store) ListJoinRequestsForItinerary(itinID string) []*models.JoinRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*models.JoinRequest
	for _, jr := range s.joinReqs {
		if jr.ItineraryID == itinID {
			list = append(list, jr)
		}
	}
	return list
}

func (s *Store) GetJoinRequest(id string) (*models.JoinRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if jr, ok := s.joinReqs[id]; ok {
		return jr, nil
	}
	return nil, errors.New("join request not found")
}

// ---------------- THREADS & MESSAGES ----------------

func (s *Store) ListThreads(userID string) []*models.Thread {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*models.Thread
	for _, t := range s.threads {
		for _, pid := range t.ParticipantIDs {
			if pid == userID {
				list = append(list, t)
				break
			}
		}
	}
	return list
}

func (s *Store) GetThread(id string) (*models.Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.threads[id]; ok {
		return t, nil
	}
	return nil, errors.New("thread not found")
}

func (s *Store) CreateThread(t *models.Thread) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[t.ID] = t
}

func (s *Store) ListMessages(threadID string, before string, limit int) []*models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages[threadID]
	if len(msgs) == 0 {
		return []*models.Message{}
	}
	if len(msgs) > limit {
		return msgs[len(msgs)-limit:]
	}
	return msgs
}

func (s *Store) AddMessage(m *models.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[m.ThreadID] = append(s.messages[m.ThreadID], m)
	if t, ok := s.threads[m.ThreadID]; ok {
		t.LastMessage = m
		t.UpdatedAt = m.CreatedAt
	}
}

// ---------------- GROUP ALBUM ----------------

func (s *Store) ListGroupPhotos(groupID string) []*models.GroupPhoto {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.photos[groupID]
}

func (s *Store) AddGroupPhoto(p *models.GroupPhoto) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.photos[p.GroupID] = append(s.photos[p.GroupID], p)
}

func (s *Store) DeleteGroupPhoto(groupID, photoID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.photos[groupID]
	var updated []*models.GroupPhoto
	found := false
	for _, p := range list {
		if p.ID == photoID {
			found = true
			continue
		}
		updated = append(updated, p)
	}
	s.photos[groupID] = updated
	return found
}

// ---------------- EXPENSES & SPLITS ----------------

func (s *Store) ListExpenses(groupID string) []*models.Expense {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.expenses[groupID]
}

func (s *Store) AddExpense(e *models.Expense) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expenses[e.GroupID] = append(s.expenses[e.GroupID], e)
}

func (s *Store) DeleteExpense(groupID, expenseID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.expenses[groupID]
	var updated []*models.Expense
	found := false
	for _, ex := range list {
		if ex.ID == expenseID {
			found = true
			continue
		}
		updated = append(updated, ex)
	}
	s.expenses[groupID] = updated
	return found
}

func (s *Store) CalculateBalances(groupID string) []models.NetBalance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	expenses := s.expenses[groupID]
	net := make(map[string]int64)
	names := make(map[string]string)

	for _, e := range expenses {
		names[e.PayerID] = e.PayerName
		net[e.PayerID] += e.AmountCents
		for uid, share := range e.Shares {
			net[uid] -= share
		}
	}

	var debtors []struct {
		id     string
		amount int64
	}
	var creditors []struct {
		id     string
		amount int64
	}

	for id, balance := range net {
		if balance < 0 {
			debtors = append(debtors, struct {
				id     string
				amount int64
			}{id: id, amount: -balance})
		} else if balance > 0 {
			creditors = append(creditors, struct {
				id     string
				amount int64
			}{id: id, amount: balance})
		}
	}

	var balances []models.NetBalance
	dIdx, cIdx := 0, 0
	for dIdx < len(debtors) && cIdx < len(creditors) {
		debtor := &debtors[dIdx]
		creditor := &creditors[cIdx]

		settleAmount := debtor.amount
		if creditor.amount < settleAmount {
			settleAmount = creditor.amount
		}

		fromName := names[debtor.id]
		if fromName == "" {
			fromName = "Member"
		}
		toName := names[creditor.id]
		if toName == "" {
			toName = "Member"
		}

		balances = append(balances, models.NetBalance{
			FromUserID:   debtor.id,
			FromUserName: fromName,
			ToUserID:     creditor.id,
			ToUserName:   toName,
			AmountCents:  settleAmount,
			Text:         fmt.Sprintf("%s owes %s $%.2f", fromName, toName, float64(settleAmount)/100.0),
		})

		debtor.amount -= settleAmount
		creditor.amount -= settleAmount

		if debtor.amount == 0 {
			dIdx++
		}
		if creditor.amount == 0 {
			cIdx++
		}
	}

	return balances
}

// ---------------- FRIENDS & INVITES ----------------

func (s *Store) ListFriends(userID string) []*models.UserSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, err := s.GetUserByID(userID)
	if err != nil || len(u.FriendIDs) == 0 {
		return []*models.UserSummary{}
	}

	var list []*models.UserSummary
	for _, fid := range u.FriendIDs {
		if fu, ok := s.users[fid.Hex()]; ok {
			list = append(list, &models.UserSummary{
				ID:          fu.ID.Hex(),
				Name:        fu.Name,
				Username:    fu.Username,
				PhotoURL:    fu.PhotoURL,
				Status:      fu.Status,
				AvatarColor: fu.AvatarColor,
			})
		}
	}
	return list
}

func (s *Store) AddFriend(userAID, userBID string) {
	uA, errA := s.GetUserByID(userAID)
	uB, errB := s.GetUserByID(userBID)
	if errA != nil || errB != nil {
		return
	}

	contains := func(slice []bson.ObjectID, item bson.ObjectID) bool {
		for _, x := range slice {
			if x == item {
				return true
			}
		}
		return false
	}

	if !contains(uA.FriendIDs, uB.ID) {
		uA.FriendIDs = append(uA.FriendIDs, uB.ID)
		_ = s.UpdateUser(uA)
	}
	if !contains(uB.FriendIDs, uA.ID) {
		uB.FriendIDs = append(uB.FriendIDs, uA.ID)
		_ = s.UpdateUser(uB)
	}
}

func (s *Store) RemoveFriend(userAID, userBID string) {
	uA, errA := s.GetUserByID(userAID)
	uB, errB := s.GetUserByID(userBID)
	if errA != nil || errB != nil {
		return
	}

	remove := func(slice []bson.ObjectID, item bson.ObjectID) []bson.ObjectID {
		var out []bson.ObjectID
		for _, x := range slice {
			if x != item {
				out = append(out, x)
			}
		}
		return out
	}

	uA.FriendIDs = remove(uA.FriendIDs, uB.ID)
	uB.FriendIDs = remove(uB.FriendIDs, uA.ID)
	_ = s.UpdateUser(uA)
	_ = s.UpdateUser(uB)
}

func (s *Store) CreateFriendRequest(fromID, toID string) (*models.FriendRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req := &models.FriendRequest{
		ID:         util.GenerateID(),
		FromUserID: fromID,
		ToUserID:   toID,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *Store) ListFriendRequests(userID string) []*models.FriendRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []*models.FriendRequest
	for _, r := range s.requests {
		if (r.ToUserID == userID || r.FromUserID == userID) && r.Status == "pending" {
			if fromU, ok := s.users[r.FromUserID]; ok {
				r.FromUser = &models.UserSummary{
					ID:          fromU.ID.Hex(),
					Name:        fromU.Name,
					Username:    fromU.Username,
					PhotoURL:    fromU.PhotoURL,
					Status:      fromU.Status,
					AvatarColor: fromU.AvatarColor,
				}
			}
			if toU, ok := s.users[r.ToUserID]; ok {
				r.ToUser = &models.UserSummary{
					ID:          toU.ID.Hex(),
					Name:        toU.Name,
					Username:    toU.Username,
					PhotoURL:    toU.PhotoURL,
					Status:      toU.Status,
					AvatarColor: toU.AvatarColor,
				}
			}
			list = append(list, r)
		}
	}
	return list
}

func (s *Store) UpdateFriendRequestStatus(id string, status string) (*models.FriendRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.requests[id]
	if !ok {
		return nil, errors.New("request not found")
	}
	r.Status = status
	if status == "accepted" {
		go s.AddFriend(r.FromUserID, r.ToUserID)
	}
	return r, nil
}

func (s *Store) CreateInvite(creatorID string) *models.InviteLink {
	s.mu.Lock()
	defer s.mu.Unlock()

	code := util.GenerateID()[:8]
	invite := &models.InviteLink{
		ID:            util.GenerateID(),
		Code:          code,
		CreatorUserID: creatorID,
		InviteURL:     fmt.Sprintf("https://sidequestz.app/join/%s", code),
		CreatedAt:     time.Now().UTC(),
		ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
	}
	s.invites[code] = invite
	return invite
}

// ---------------- SEED CATALOG (MATCHING EXACT EVENT SCHEMA) ----------------

func (s *Store) seedDefaultCatalog() {
	s.places = []models.Place{
		{
			ID:             "place_1",
			Name:           "Piedmont Park",
			Address:        "1320 Monroe Dr NE, Atlanta, GA 30306",
			Lat:            33.7879,
			Lng:            -84.3733,
			Category:       "park",
			SuggestionChip: "Great for walks & picnics",
			Tags:           []string{"nature", "outdoors", "chill", "free"},
		},
		{
			ID:             "place_2",
			Name:           "Ponce City Market",
			Address:        "675 Ponce De Leon Ave NE, Atlanta, GA 30308",
			Lat:            33.7724,
			Lng:            -84.3656,
			Category:       "food_hall",
			SuggestionChip: "Rooftop games & top food hall",
			Tags:           []string{"foodie", "shopping", "views", "drinks"},
		},
		{
			ID:             "place_3",
			Name:           "High Museum of Art",
			Address:        "1280 Peachtree St NE, Atlanta, GA 30309",
			Lat:            33.7904,
			Lng:            -84.3853,
			Category:       "museum",
			SuggestionChip: "Midtown cultural gem",
			Tags:           []string{"arts", "culture", "indoor"},
		},
		{
			ID:             "place_4",
			Name:           "Atlanta BeltLine Eastside Trail",
			Address:        "10th St NE & Monroe Dr NE, Atlanta, GA 30306",
			Lat:            33.7820,
			Lng:            -84.3680,
			Category:       "trail",
			SuggestionChip: "Biking, murals, and food spots",
			Tags:           []string{"outdoors", "murals", "fitness", "chill"},
		},
	}

	now := time.Now().UTC()
	start1 := now.Add(24 * time.Hour)
	start2 := now.Add(48 * time.Hour)
	expires1 := start1.Add(8 * time.Hour)
	expires2 := start2.Add(8 * time.Hour)

	// Seed exact events from schema
	id1, _ := bson.ObjectIDFromHex("6ab72395aa01f0e712679d9b")
	id2, _ := bson.ObjectIDFromHex("6ab72395aa01f0e712679d9c")

	s.events = []models.Event{
		{
			ID:             id1,
			Kind:           "event",
			City:           "atlanta",
			Name:           "camoufly (18+ Event)",
			Summary:        nil,
			Description:    nil,
			Category:       "other",
			SourceCategory: nil,
			Tags:           []string{"music", "nightlife", "electronic"},
			Location: models.GeoJSONPoint{
				Type:        "Point",
				Coordinates: []float64{-84.375397, 33.744301}, // lng FIRST
			},
			Address: models.EventAddress{
				Street:      ptrString("181 Ralph David Abernathy Blvd"),
				Locality:    ptrString("Atlanta"),
				Region:      ptrString("GA"),
				PostalCode:  ptrString("30312"),
				CountryCode: ptrString("US"),
			},
			VenueName:  ptrString("Wish Lounge at Believe Music Hall"),
			Start:      start1,
			End:        nil,
			Attendance: "fixed_start",
			Timezone:   "America/New_York",
			Duration: &models.EventDuration{
				MedianMin: 60.0,
				Sigma:     0.5,
				P75Min:    84.0,
				Source:    "category_prior",
			},
			Price: &models.EventPrice{
				Tier:     2,
				Cents:    2500,
				Currency: "USD",
				IsFree:   false,
			},
			URL:       "https://www.ticketmaster.com/event/Z7r9jZ1A7JoPd",
			TicketURL: "https://www.ticketmaster.com/event/Z7r9jZ1A7JoPd",
			ImageURL:  "https://s1.ticketm.net/dam/c/f51/ee785ed6-f806-4195-98f4-69f67e09df51_106201_TABLET_LANDSCAPE_LARGE_16_9.jpg",
			SourceKeys: []string{
				"ticketmaster:Z7r9jZ1A7JoPd",
			},
			Sources: []models.EventSource{
				{
					Name:      "ticketmaster",
					ID:        "Z7r9jZ1A7JoPd",
					URL:       "https://www.ticketmaster.com/event/Z7r9jZ1A7JoPd",
					FetchedAt: now,
				},
			},
			ExpiresAt: &expires1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:             id2,
			Kind:           "event",
			City:           "atlanta",
			Name:           "Collect-A-Con",
			Summary:        nil,
			Description:    nil,
			Category:       "other",
			SourceCategory: nil,
			Tags:           []string{"expo", "collectibles", "pop_culture"},
			Location: models.GeoJSONPoint{
				Type:        "Point",
				Coordinates: []float64{-84.398201, 33.758301}, // lng FIRST
			},
			Address: models.EventAddress{
				Street:      ptrString("Georgia World Congress Center"),
				Locality:    ptrString("Atlanta"),
				Region:      ptrString("GA"),
				PostalCode:  ptrString("30313"),
				CountryCode: ptrString("US"),
			},
			VenueName:  ptrString("Georgia World Congress Center"),
			Start:      start2,
			End:        nil,
			Attendance: "fixed_start",
			Timezone:   "America/New_York",
			Duration: &models.EventDuration{
				MedianMin: 180.0,
				Sigma:     0.5,
				P75Min:    240.0,
				Source:    "category_prior",
			},
			Price: &models.EventPrice{
				Tier:     2,
				Cents:    3000,
				Currency: "USD",
				IsFree:   false,
			},
			URL:       "https://collect-a-con.com/atlanta",
			TicketURL: "https://collect-a-con.com/atlanta",
			ImageURL:  "https://images.unsplash.com/photo-1563089145-599997674d42?w=800",
			SourceKeys: []string{
				"collectacon:atlanta",
			},
			Sources: []models.EventSource{
				{
					Name:      "collectacon",
					ID:        "atlanta",
					URL:       "https://collect-a-con.com/atlanta",
					FetchedAt: now,
				},
			},
			ExpiresAt: &expires2,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}
