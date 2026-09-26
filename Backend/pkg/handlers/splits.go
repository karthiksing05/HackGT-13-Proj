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

	"github.com/gorilla/mux"
)

type CreateExpenseRequest struct {
	What                string   `json:"what"`
	AmountCents         int64    `json:"amount_cents"`
	PayerID             string   `json:"payer_id"`
	SplitBetweenUserIDs []string `json:"split_between_user_ids"`
}

type SettleBalanceRequest struct {
	ToUserID        string `json:"to_user_id"`
	AmountCents     int64  `json:"amount_cents"`
	PaymentMethodID string `json:"payment_method_id,omitempty"`
}

func ListExpenses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]

	expenses := store.GlobalStore.ListExpenses(groupID)
	if expenses == nil {
		expenses = []*models.Expense{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"expenses": expenses,
		"count":    len(expenses),
	})
}

func CreateExpense(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]
	uid := middleware.GetUserID(r)

	var req CreateExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AmountCents <= 0 {
		middleware.WriteError(w, http.StatusBadRequest, "Amount must be greater than zero")
		return
	}
	if len(req.SplitBetweenUserIDs) == 0 {
		middleware.WriteError(w, http.StatusBadRequest, "split_between_user_ids must not be empty")
		return
	}

	payerID := req.PayerID
	if payerID == "" {
		payerID = uid
	}
	payerName := "Group Member"
	if u, err := store.GlobalStore.GetUserByID(payerID); err == nil {
		payerName = u.Name
	}

	// Server owns equal-split math with 1¢ rounding
	n := int64(len(req.SplitBetweenUserIDs))
	baseShare := req.AmountCents / n
	remainder := req.AmountCents % n

	shares := make(map[string]int64)
	for i, memberID := range req.SplitBetweenUserIDs {
		share := baseShare
		if int64(i) < remainder {
			share++ // Distribute 1¢ remainder evenly
		}
		shares[memberID] = share
	}

	expense := &models.Expense{
		ID:                  util.GenerateID(),
		GroupID:             groupID,
		PayerID:             payerID,
		PayerName:           payerName,
		What:                req.What,
		AmountCents:         req.AmountCents,
		SplitBetweenUserIDs: req.SplitBetweenUserIDs,
		Shares:              shares,
		CreatedAt:           time.Now().UTC(),
	}

	store.GlobalStore.AddExpense(expense)

	// Notify group members over WebSocket
	realtime.GlobalHub.SendToUsers(req.SplitBetweenUserIDs, "group:expense_created", expense)

	middleware.WriteJSON(w, http.StatusCreated, expense)
}

func DeleteExpense(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]
	expenseID := vars["expenseId"]

	if !store.GlobalStore.DeleteExpense(groupID, expenseID) {
		middleware.WriteError(w, http.StatusNotFound, "Expense not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Expense removed",
	})
}

func GetGroupBalances(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]

	balances := store.GlobalStore.CalculateBalances(groupID)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"balances": balances,
		"count":    len(balances),
	})
}

func SettleGroupBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]
	uid := middleware.GetUserID(r)

	var req SettleBalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AmountCents <= 0 || req.ToUserID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid amount or recipient")
		return
	}

	payer, _ := store.GlobalStore.GetUserByID(uid)
	payerName := "Member"
	if payer != nil {
		payerName = payer.Name
	}

	// Create settlement expense that negates the balance
	settlementExpense := &models.Expense{
		ID:                  util.GenerateID(),
		GroupID:             groupID,
		PayerID:             uid,
		PayerName:           payerName,
		What:                fmt.Sprintf("Payment to Settle Balance (Visa)"),
		AmountCents:         req.AmountCents,
		SplitBetweenUserIDs: []string{req.ToUserID},
		Shares: map[string]int64{
			req.ToUserID: req.AmountCents,
		},
		CreatedAt: time.Now().UTC(),
	}

	store.GlobalStore.AddExpense(settlementExpense)

	// Notify recipient
	realtime.GlobalHub.SendToUser(req.ToUserID, "group:settled", map[string]interface{}{
		"from_user_id": uid,
		"amount_cents": req.AmountCents,
		"status":       "paid_via_visa",
	})

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "settled",
		"amount_cents": req.AmountCents,
		"settled_at":   time.Now().UTC(),
		"message":      "Payment completed with Visa",
	})
}
