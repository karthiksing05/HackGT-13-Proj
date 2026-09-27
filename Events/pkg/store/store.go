package store

import (
	"context"
	"errors"
	"events/pkg/config"
	"events/pkg/models"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrSoldOut        = errors.New("sold out")
	ErrQuoteExpired   = errors.New("quote expired")
	ErrReplayedNonce  = errors.New("nonce replayed")
	ErrDuplicateOrder = errors.New("order already exists")
)

// Store defines persistence operations for the Events merchant server.
type Store interface {
	GetEvent(ctx context.Context, slug string) (*models.Event, error)
	ListEvents(ctx context.Context, category string, search string) ([]*models.Event, error)
	SaveQuote(ctx context.Context, quote *models.Quote) error
	GetQuote(ctx context.Context, quoteID string) (*models.Quote, error)
	SaveOrder(ctx context.Context, order *models.OrderConfirmation, idempotencyKey string) error
	GetOrder(ctx context.Context, orderID string) (*models.OrderConfirmation, error)
	GetOrderByTicketID(ctx context.Context, ticketID string) (*models.OrderConfirmation, error)
	GetOrderByBarcode(ctx context.Context, barcode string) (*models.OrderConfirmation, error)
	GetOrderByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.OrderConfirmation, error)
	ReserveTickets(ctx context.Context, slug string, quantity int) error
	ReleaseTickets(ctx context.Context, slug string, quantity int) error
	CheckAndRecordNonce(nonce string, expiresAt time.Time) error
	RecordRejectedRequest(ctx context.Context, req *models.RejectedRequest) error
	GetScenario(ctx context.Context) string
	SetScenario(ctx context.Context, scenario string)
}

// NewStore initializes a Store, preferring MongoDB if reachable, and falling back
// to a high-fidelity in-memory store if MongoDB is not running locally.
func NewStore(ctx context.Context, cfg *config.Config) (Store, error) {
	if cfg.MongoURI != "" {
		ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
		if err == nil {
			if pingErr := client.Ping(ctxTimeout, nil); pingErr == nil {
				log.Info().Str("db", cfg.MongoDB).Msg("Connected to MongoDB for Events merchant")
				mStore := NewMongoStore(client.Database(cfg.MongoDB))
				if err := mStore.EnsureIndexesAndSeed(ctx); err != nil {
					log.Warn().Err(err).Msg("failed to seed mongo; falling back to memory store")
				} else {
					if cfg.CatalogCollection != "" {
						src := CatalogSource{DB: cfg.CatalogDB, Collection: cfg.CatalogCollection}
						if n, err := mStore.SeedFromCatalog(ctx, src); err != nil {
							log.Warn().Err(err).Msg("seeding listings from the catalog failed")
						} else {
							log.Info().Int("listings", n).Str("catalog", src.DB+"."+src.Collection).Msg("Seeded catalog listings")
						}
					}
					return mStore, nil
				}
			} else {
				log.Info().Msg("MongoDB not reachable, using in-memory store with seeded catalog")
			}
		} else {
			log.Info().Msg("MongoDB connect failed, using in-memory store with seeded catalog")
		}
	}

	mem := NewMemoryStore()
	mem.SeedDefaultEvents()
	return mem, nil
}
