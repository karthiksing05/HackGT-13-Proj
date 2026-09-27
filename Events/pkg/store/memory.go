package store

import (
	"context"
	"events/pkg/models"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-memory implementation of Store suitable for local dev, demos and tests.
type MemoryStore struct {
	mu           sync.RWMutex
	events       map[string]*models.Event
	quotes       map[string]*models.Quote
	orders       map[string]*models.OrderConfirmation
	ordersByTkt  map[string]*models.OrderConfirmation
	ordersByIdem map[string]*models.OrderConfirmation
	orderHistory []*models.OrderConfirmation
	nonces       map[string]time.Time
	rejected     []models.RejectedRequest
	scenario     string
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events:       make(map[string]*models.Event),
		quotes:       make(map[string]*models.Quote),
		orders:       make(map[string]*models.OrderConfirmation),
		ordersByTkt:  make(map[string]*models.OrderConfirmation),
		ordersByIdem: make(map[string]*models.OrderConfirmation),
		orderHistory: make([]*models.OrderConfirmation, 0),
		nonces:       make(map[string]time.Time),
		rejected:     make([]models.RejectedRequest, 0),
		scenario:     models.ScenarioNormal,
	}
}

// SeedDefaultEvents populates the store with all 25 Saltlight Harbor ticketed events.
func (m *MemoryStore) SeedDefaultEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range DefaultDemoEvents {
		if models.IsReservedSlug(e.Slug) {
			continue
		}
		// Create a copy
		copyEvent := *e
		m.events[e.Slug] = &copyEvent
	}
}

func (m *MemoryStore) GetEvent(ctx context.Context, slug string) (*models.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if models.IsReservedSlug(slug) {
		return nil, ErrNotFound
	}
	e, ok := m.events[slug]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *MemoryStore) ListEvents(ctx context.Context, category string, search string) ([]*models.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []*models.Event
	searchLower := strings.ToLower(strings.TrimSpace(search))

	for _, e := range m.events {
		if category != "" && category != "all" && !strings.EqualFold(e.Category, category) {
			continue
		}
		if searchLower != "" {
			titleMatch := strings.Contains(strings.ToLower(e.Title), searchLower)
			venueMatch := strings.Contains(strings.ToLower(e.Venue), searchLower)
			descMatch := strings.Contains(strings.ToLower(e.Description), searchLower)
			if !titleMatch && !venueMatch && !descMatch {
				continue
			}
		}
		cp := *e
		res = append(res, &cp)
	}
	return res, nil
}

func (m *MemoryStore) SaveQuote(ctx context.Context, quote *models.Quote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *quote
	m.quotes[quote.QuoteID] = &cp
	return nil
}

func (m *MemoryStore) GetQuote(ctx context.Context, quoteID string) (*models.Quote, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotes[quoteID]
	if !ok {
		return nil, ErrNotFound
	}
	if time.Now().After(q.ExpiresAt) {
		return nil, ErrQuoteExpired
	}
	cp := *q
	return &cp, nil
}

func (m *MemoryStore) SaveOrder(ctx context.Context, order *models.OrderConfirmation, idempotencyKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idempotencyKey != "" && m.ordersByIdem[idempotencyKey] != nil {
		return ErrDuplicateOrder
	}
	cp := *order
	cp.IdempotencyKey = idempotencyKey
	m.orders[order.OrderID] = &cp
	if order.Ticket.TicketID != "" {
		m.ordersByTkt[order.Ticket.TicketID] = &cp
	}
	if idempotencyKey != "" {
		m.ordersByIdem[idempotencyKey] = &cp
	}
	m.orderHistory = append(m.orderHistory, &cp)
	return nil
}

func (m *MemoryStore) GetOrder(ctx context.Context, orderID string) (*models.OrderConfirmation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (m *MemoryStore) GetOrderByTicketID(ctx context.Context, ticketID string) (*models.OrderConfirmation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.ordersByTkt[ticketID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (m *MemoryStore) GetOrderByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.OrderConfirmation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.ordersByIdem[idempotencyKey]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (m *MemoryStore) ReserveTickets(ctx context.Context, slug string, quantity int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[slug]
	if !ok {
		return ErrNotFound
	}
	if e.Remaining < quantity {
		return ErrSoldOut
	}
	e.Remaining -= quantity
	return nil
}

func (m *MemoryStore) ReleaseTickets(ctx context.Context, slug string, quantity int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[slug]
	if !ok {
		return ErrNotFound
	}
	e.Remaining += quantity
	if e.Remaining > e.Capacity {
		e.Remaining = e.Capacity
	}
	return nil
}

func (m *MemoryStore) CheckAndRecordNonce(nonce string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	// Clean expired nonces
	for k, exp := range m.nonces {
		if now.After(exp) {
			delete(m.nonces, k)
		}
	}
	if _, exists := m.nonces[nonce]; exists {
		return ErrReplayedNonce
	}
	m.nonces[nonce] = expiresAt
	return nil
}

func (m *MemoryStore) RecordRejectedRequest(ctx context.Context, req *models.RejectedRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = append([]models.RejectedRequest{*req}, m.rejected...)
	if len(m.rejected) > 50 {
		m.rejected = m.rejected[:50]
	}
	return nil
}

func (m *MemoryStore) ListRejectedRequests(ctx context.Context, limit int) ([]models.RejectedRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.rejected) {
		limit = len(m.rejected)
	}
	res := make([]models.RejectedRequest, limit)
	copy(res, m.rejected[:limit])
	return res, nil
}

func (m *MemoryStore) ListRecentOrders(ctx context.Context, limit int) ([]*models.OrderConfirmation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.orderHistory)
	if limit <= 0 || limit > n {
		limit = n
	}
	res := make([]*models.OrderConfirmation, 0, limit)
	for i := n - 1; i >= n-limit && i >= 0; i-- {
		cp := *m.orderHistory[i]
		res = append(res, &cp)
	}
	return res, nil
}

func (m *MemoryStore) GetScenario(ctx context.Context) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scenario
}

func (m *MemoryStore) SetScenario(ctx context.Context, scenario string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scenario = scenario
}

func (m *MemoryStore) TotalOrders(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.orderHistory)
}

func (m *MemoryStore) TotalGrossCents(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, o := range m.orderHistory {
		total += o.TotalCents
	}
	return total
}
