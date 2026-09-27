package store

import (
	"context"
	"events/pkg/models"
	"math"
	"regexp"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CatalogSource names the SideQuestz activity catalog whose paid items this
// site sells (freetime.pitch_activities: the Atlanta pitch catalog). Each
// item with a ticketUrl of https://events.sidequestz.tech/{slug}/tickets
// becomes the listing {slug}. See dataingestion/pitch/TICKETS_HANDOFF.md.
type CatalogSource struct {
	DB         string
	Collection string
}

// Places (restaurants, bars, museums, …) have no showtime: their reservation,
// cover or pass is valid any day of the pitch range, Sep 27 – Oct 10, 2026 in
// Atlanta, so that range is their start and end (pitch/tickets.py).
var (
	PassRangeStart = time.Date(2026, 9, 27, 0, 0, 0, 0, atlanta)
	PassRangeEnd   = time.Date(2026, 10, 10, 23, 59, 0, 0, atlanta)
)

// atlanta is the catalog's time zone.
var atlanta = func() *time.Location {
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		return loc
	}
	return time.FixedZone("EDT", -4*3600)
}()

var ticketURLRe = regexp.MustCompile(`^https://events\.sidequestz\.tech/([a-z0-9]+(?:-[a-z0-9]+)*)/tickets$`)

// eventCapacity is a rough room size per category for events; places get
// placeCapacity (pitch/tickets.py).
var eventCapacity = map[string]int{
	"comedy": 120, "theater": 400, "cinema": 150, "live_music": 250, "nightclub": 300, "bar": 120,
	"restaurant": 60, "class_workshop": 20, "tour": 25, "festival": 1000, "sports_event": 500,
	"community_event": 150, "market": 500,
}

const (
	defaultEventCapacity = 200
	placeCapacity        = 1000
)

// CatalogItem is the part of a catalog activity a listing needs.
type CatalogItem struct {
	Kind        string     `bson:"kind"`
	Name        string     `bson:"name"`
	Summary     string     `bson:"summary"`
	Description string     `bson:"description"`
	Category    string     `bson:"category"`
	Tags        []string   `bson:"tags"`
	VenueName   string     `bson:"venueName"`
	Start       *time.Time `bson:"start"`
	End         *time.Time `bson:"end"`
	TicketURL   string     `bson:"ticketUrl"`
	Address     struct {
		Formatted string `bson:"formatted"`
	} `bson:"address"`
	Price *struct {
		Min *float64 `bson:"min"`
		Max *float64 `bson:"max"`
	} `bson:"price"`
}

// ListingFromCatalog maps a paid catalog item onto a listing. ok is false for
// items the site can't sell: no ticket link on this host, no price, a
// reserved slug, or an event without times. The price follows the app, which
// budgets a stop at price.min: UnitCents is the minimum, MaxUnitCents the top.
func ListingFromCatalog(it *CatalogItem) (e *models.Event, ok bool) {
	m := ticketURLRe.FindStringSubmatch(it.TicketURL)
	if m == nil || models.IsReservedSlug(m[1]) || it.Price == nil || it.Price.Min == nil {
		return nil, false
	}
	minC := cents(*it.Price.Min)
	maxC := minC
	if it.Price.Max != nil {
		maxC = cents(*it.Price.Max)
	}
	e = &models.Event{
		Slug:         m[1],
		Title:        it.Name,
		Description:  it.Description,
		Summary:      it.Summary,
		Category:     it.Category,
		Tags:         it.Tags,
		Venue:        it.VenueName,
		Address:      it.Address.Formatted,
		UnitCents:    minC,
		MaxUnitCents: maxC,
	}
	if e.Venue == "" {
		e.Venue = it.Name
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if it.Kind == "event" {
		if it.Start == nil || it.End == nil {
			return nil, false
		}
		e.Start, e.End = it.Start.UTC(), it.End.UTC()
		e.Capacity = defaultEventCapacity
		if c, ok := eventCapacity[it.Category]; ok {
			e.Capacity = c
		}
	} else {
		e.Start, e.End = PassRangeStart.UTC(), PassRangeEnd.UTC()
		e.Capacity = placeCapacity
	}
	e.Remaining = e.Capacity
	return e, true
}

func cents(dollars float64) int { return int(math.Round(dollars * 100)) }

// SeedFromCatalog upserts a listing for every paid item of src into
// merchant_events. Listing fields follow the catalog on every start; capacity
// and remaining are set only when a listing is first created, so tickets
// already sold stay sold. It returns how many listings it wrote.
func (m *MongoStore) SeedFromCatalog(ctx context.Context, src CatalogSource) (int, error) {
	cur, err := m.db.Client().Database(src.DB).Collection(src.Collection).Find(ctx,
		bson.M{"ticketUrl": bson.M{"$type": "string"}},
		options.Find().SetProjection(bson.M{"embedding": 0, "embeddingText": 0}))
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	var writes []mongo.WriteModel
	skipped := 0
	for cur.Next(ctx) {
		var it CatalogItem
		if err := cur.Decode(&it); err != nil {
			skipped++
			continue
		}
		e, ok := ListingFromCatalog(&it)
		if !ok {
			skipped++
			continue
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"slug": e.Slug}).
			SetUpdate(bson.M{
				"$set": bson.M{
					"title": e.Title, "description": e.Description, "summary": e.Summary,
					"category": e.Category, "tags": e.Tags, "venue": e.Venue, "address": e.Address,
					"start": e.Start, "end": e.End, "unitCents": e.UnitCents, "maxUnitCents": e.MaxUnitCents,
					"imageUrl": e.ImageURL,
				},
				"$setOnInsert": bson.M{"capacity": e.Capacity, "remaining": e.Remaining},
			}).
			SetUpsert(true))
	}
	if err := cur.Err(); err != nil {
		return 0, err
	}
	if skipped > 0 {
		log.Warn().Int("skipped", skipped).Str("catalog", src.DB+"."+src.Collection).Msg("catalog items without a sellable listing")
	}
	if len(writes) == 0 {
		return 0, nil
	}
	if _, err := m.db.Collection(CollEvents).BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false)); err != nil {
		return 0, err
	}
	return len(writes), nil
}
