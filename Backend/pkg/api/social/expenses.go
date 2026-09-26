package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"net/http"
	"slices"
	"strings"

	"github.com/gorilla/mux"
)

// Sentences of the Splits tab.
const (
	MsgExpenseWhat    = "Add what the expense was for."
	MsgExpenseAmount  = "Enter an amount above $0."
	MsgExpenseSplit   = "Pick at least one person to split with."
	MsgExpensePayer   = "Pick who paid from this group."
	MsgExpenseMembers = "Split only with people in this group."
	MsgExpenseOwner   = "You can only delete expenses you added."
	MsgBalanceChanged = "The balance changed. Check the new amount and try again."
	MsgNeedCard       = "Add a card in Account first."
)

// equalShares splits total cents n ways: floor(total / n) each, and the
// first total mod n people pay 1¢ more (SplitMath.equalShares).
func equalShares(total, n int) []int {
	if n <= 0 || total < 0 {
		return []int{}
	}
	base := total / n
	extra := total - base*n
	shares := make([]int, n)
	for i := range shares {
		shares[i] = base
		if i < extra {
			shares[i]++
		}
	}
	return shares
}

// balances is each other member's net against me: + they owe me, − I owe
// them (MockLedger.balances). Only current members are listed.
func balances(expenses []*models.Expense, me string, members []string) []contract.Balance {
	net := map[string]int{}
	for _, e := range expenses {
		shares := e.Shares
		if len(shares) != len(e.SplitAmong) {
			shares = equalShares(e.AmountCents, len(e.SplitAmong))
		}
		for i, member := range e.SplitAmong {
			switch {
			case member == e.PayerID:
			case e.PayerID == me:
				net[member] += shares[i]
			case member == me:
				net[e.PayerID] -= shares[i]
			}
		}
	}
	out := []contract.Balance{}
	for _, member := range members {
		if member != me {
			out = append(out, contract.Balance{UserID: member, NetCents: net[member]})
		}
	}
	return out
}

// ListExpenses is GET /groups/{id}/expenses → [Expense], oldest first.
func (h *H) ListExpenses(w http.ResponseWriter, r *http.Request) {
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], api.UserID(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	list, err := h.d.Store.Expenses().List(r.Context(), th.ID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.Expense, 0, len(list))
	for _, e := range list {
		out = append(out, expenseView(e))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AddExpense is POST /groups/{id}/expenses (NewExpense) → 201 Expense with
// equal shares; members hear expense.added and get the new balance chips.
func (h *H) AddExpense(w http.ResponseWriter, r *http.Request) {
	var req contract.NewExpense
	if !httpx.Decode(w, r, &req) {
		return
	}
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	what := strings.TrimSpace(req.What)
	split := dedupe(req.SplitAmong)
	switch {
	case what == "":
		httpx.Error(w, http.StatusBadRequest, MsgExpenseWhat)
		return
	case req.AmountCents <= 0:
		httpx.Error(w, http.StatusBadRequest, MsgExpenseAmount)
		return
	case len(split) == 0:
		httpx.Error(w, http.StatusBadRequest, MsgExpenseSplit)
		return
	case !slices.Contains(th.MemberIDs, req.PayerID):
		httpx.Error(w, http.StatusBadRequest, MsgExpensePayer)
		return
	}
	for _, id := range split {
		if !slices.Contains(th.MemberIDs, id) {
			httpx.Error(w, http.StatusBadRequest, MsgExpenseMembers)
			return
		}
	}
	exp := &models.Expense{
		GroupID: th.ID, What: what, AmountCents: req.AmountCents, PayerID: req.PayerID,
		SplitAmong: split, Shares: equalShares(req.AmountCents, len(split)), CreatedBy: viewerID,
	}
	if err := h.d.Store.Expenses().Insert(r.Context(), exp); err != nil {
		api.Fail(w, r, err)
		return
	}
	out := expenseView(exp)
	realtime.ExpenseAdded(h.d.Publish(), th.MemberIDs, th.ID, out)
	h.pushThread(r.Context(), th, httpx.TZ(r), th.MemberIDs)
	httpx.JSON(w, http.StatusCreated, out)
}

// DeleteExpense is DELETE /groups/{id}/expenses/{expenseId}: only whoever
// added it (403 otherwise) → 204.
func (h *H) DeleteExpense(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	vars := mux.Vars(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), vars["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	exp, err := h.d.Store.Expenses().Get(r.Context(), th.ID, vars["expenseId"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if exp.CreatedBy != viewerID {
		httpx.Error(w, http.StatusForbidden, MsgExpenseOwner)
		return
	}
	if err := h.d.Store.Expenses().Delete(r.Context(), th.ID, exp.ID, viewerID); err != nil {
		api.Fail(w, r, err)
		return
	}
	h.pushThread(r.Context(), th, httpx.TZ(r), th.MemberIDs)
	httpx.NoContent(w)
}

// Balances is GET /groups/{id}/balances → [Balance] for every other member.
func (h *H) Balances(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().GroupForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	list, err := h.d.Store.Expenses().List(r.Context(), th.ID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, balances(list, viewerID, th.MemberIDs))
}

// Settle is POST /groups/{id}/settle {amount_cents, payment_method_id?}:
// the amount must be what the viewer owes right now (409 otherwise), and a
// saved card pays it: one settlement row per person owed.
//
// Simulated: no money moves. The card only labels the settlement rows
// ("Settled up with Visa •••• 4242"), like the rest of the demo payments.
func (h *H) Settle(w http.ResponseWriter, r *http.Request) {
	var req contract.SettleRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	viewerID := api.UserID(r)
	ctx := r.Context()
	th, err := h.d.Store.Threads().GroupForMember(ctx, mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	list, err := h.d.Store.Expenses().List(ctx, th.ID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	owed := 0
	debts := []contract.Balance{}
	for _, b := range balances(list, viewerID, th.MemberIDs) {
		if b.NetCents < 0 {
			owed -= b.NetCents
			debts = append(debts, b)
		}
	}
	if req.AmountCents != owed {
		httpx.Error(w, http.StatusConflict, MsgBalanceChanged)
		return
	}
	cards, err := h.d.Store.Payments().List(ctx, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	card := pickCard(cards, req.PaymentMethodID)
	if card == nil {
		httpx.Error(w, http.StatusBadRequest, MsgNeedCard)
		return
	}
	rows := make([]*models.Expense, 0, len(debts))
	for _, debt := range debts {
		amount := -debt.NetCents
		rows = append(rows, &models.Expense{
			GroupID: th.ID, What: "Settled up with " + card.Brand + " •••• " + card.Last4, AmountCents: amount,
			PayerID: viewerID, SplitAmong: []string{debt.UserID}, Shares: []int{amount}, CreatedBy: viewerID, Settlement: true,
		})
	}
	if err := h.d.Store.Expenses().Insert(ctx, rows...); err != nil {
		api.Fail(w, r, err)
		return
	}
	for _, row := range rows {
		realtime.ExpenseAdded(h.d.Publish(), th.MemberIDs, th.ID, expenseView(row))
	}
	if len(rows) > 0 {
		h.pushThread(ctx, th, httpx.TZ(r), th.MemberIDs)
	}
	httpx.NoContent(w)
}

// pickCard is the chosen card, else the default one, else the oldest (the
// default when none is flagged).
func pickCard(cards []*models.PaymentMethod, id *string) *models.PaymentMethod {
	if id != nil {
		for _, c := range cards {
			if c.ID == *id {
				return c
			}
		}
	}
	for _, c := range cards {
		if c.IsDefault {
			return c
		}
	}
	if len(cards) > 0 {
		return cards[0]
	}
	return nil
}

// dedupe keeps the first occurrence of each id, in order.
func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
