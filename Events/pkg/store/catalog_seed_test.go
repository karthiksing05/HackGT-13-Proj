package store

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func ptr[T any](v T) *T { return &v }

func catalogItem(kind, slug string, lo, hi float64) *CatalogItem {
	it := &CatalogItem{
		Kind: kind, Name: "Item " + slug, Category: "live_music", VenueName: "The EARL",
		TicketURL: "https://events.sidequestz.tech/" + slug + "/tickets",
	}
	it.Address.Formatted = "488 Flat Shoals Ave SE, Atlanta, GA 30316"
	it.Price = &struct {
		Min *float64 `bson:"min"`
		Max *float64 `bson:"max"`
	}{ptr(lo), ptr(hi)}
	return it
}

func TestListingFromCatalogEvent(t *testing.T) {
	it := catalogItem("event", "bluegrass-brunch-sep-27", 20, 30)
	start := time.Date(2026, 9, 27, 15, 30, 0, 0, time.UTC)
	it.Start, it.End = ptr(start), ptr(start.Add(2*time.Hour))

	e, ok := ListingFromCatalog(it)
	if !ok {
		t.Fatal("event not listed")
	}
	if e.Slug != "bluegrass-brunch-sep-27" || e.UnitCents != 2000 || e.MaxUnitCents != 3000 {
		t.Fatalf("slug/price: %+v", e)
	}
	if !e.Start.Equal(start) || e.Capacity != 250 || e.Remaining != 250 || e.Venue != "The EARL" {
		t.Fatalf("time/capacity: %+v", e)
	}
}

func TestListingFromCatalogPlaceSpansTheRange(t *testing.T) {
	e, ok := ListingFromCatalog(catalogItem("place", "mjq-concourse", 19.99, 30))
	if !ok {
		t.Fatal("place not listed")
	}
	if e.UnitCents != 1999 || e.Capacity != placeCapacity {
		t.Fatalf("price/capacity: %+v", e)
	}
	// Midnight Sep 27 to 11:59 PM Oct 10, Atlanta time (EDT).
	if got := e.Start.Format(time.RFC3339); got != "2026-09-27T04:00:00Z" {
		t.Errorf("start %s", got)
	}
	if got := e.End.Format(time.RFC3339); got != "2026-10-11T03:59:00Z" {
		t.Errorf("end %s", got)
	}
}

func TestListingFromCatalogRejects(t *testing.T) {
	noTimes := catalogItem("event", "no-times", 10, 10)
	otherHost := catalogItem("place", "x", 10, 10)
	otherHost.TicketURL = "https://tickets.example.com/x/tickets"
	reserved := catalogItem("place", "api", 10, 10)
	noPrice := catalogItem("place", "no-price", 10, 10)
	noPrice.Price = nil
	for name, it := range map[string]*CatalogItem{"no times": noTimes, "other host": otherHost, "reserved": reserved, "no price": noPrice} {
		if _, ok := ListingFromCatalog(it); ok {
			t.Errorf("%s: listed", name)
		}
	}
}

func TestMongoStoreSeedFromCatalog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017"))
	if err != nil {
		t.Skip("local mongodb not reachable")
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skip("local mongodb not pingable")
	}
	db := client.Database("sidequestz_events_catalog_test")
	cat := client.Database("sidequestz_catalog_test")
	defer func() { _ = db.Drop(context.Background()); _ = cat.Drop(context.Background()) }()

	start := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	_, err = cat.Collection("pitch_activities").InsertMany(ctx, []any{
		bson.M{"kind": "event", "name": "Beer & Biscuits Brunch", "venueName": "Monday Night Brewing", "category": "restaurant",
			"start": start, "end": start.Add(2 * time.Hour), "price": bson.M{"min": 25.0, "max": 30.0, "isFree": false},
			"ticketUrl": "https://events.sidequestz.tech/beer-biscuits-brunch/tickets"},
		bson.M{"kind": "place", "name": "Zoo Atlanta", "venueName": "Zoo Atlanta", "category": "zoo_aquarium",
			"price":     bson.M{"min": 30.0, "max": 35.0, "isFree": false},
			"ticketUrl": "https://events.sidequestz.tech/zoo-atlanta/tickets"},
		bson.M{"kind": "place", "name": "Piedmont Park", "price": bson.M{"min": 0.0, "isFree": true}, "ticketUrl": nil},
	})
	if err != nil {
		t.Fatal(err)
	}

	st := NewMongoStore(db)
	if err := st.EnsureIndexesAndSeed(ctx); err != nil {
		t.Fatal(err)
	}
	src := CatalogSource{DB: cat.Name(), Collection: "pitch_activities"}
	if n, err := st.SeedFromCatalog(ctx, src); err != nil || n != 2 {
		t.Fatalf("seed: n=%d err=%v", n, err)
	}

	// A sale survives a reseed; a price change doesn't wait for one.
	if err := st.ReserveTickets(ctx, "zoo-atlanta", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Collection("pitch_activities").UpdateOne(ctx, bson.M{"name": "Zoo Atlanta"}, bson.M{"$set": bson.M{"price.min": 32.0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SeedFromCatalog(ctx, src); err != nil {
		t.Fatal(err)
	}
	zoo, err := st.GetEvent(ctx, "zoo-atlanta")
	if err != nil {
		t.Fatal(err)
	}
	if zoo.Remaining != placeCapacity-3 || zoo.UnitCents != 3200 {
		t.Fatalf("after reseed: remaining=%d unit=%d", zoo.Remaining, zoo.UnitCents)
	}
	if _, err := st.GetEvent(ctx, "beer-biscuits-brunch"); err != nil {
		t.Fatalf("event listing: %v", err)
	}
	all, _ := st.ListEvents(ctx, "", "")
	if len(all) != len(DefaultDemoEvents)+2 {
		t.Fatalf("listings: %d", len(all))
	}
}
