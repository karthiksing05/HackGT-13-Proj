package realtime

import "Backend/pkg/contract"

// Event types (frontend WebSocketService.swift decodes exactly these).
const (
	EventConnected        = "connected"
	EventMessageNew       = "message.new"
	EventThreadUpdated    = "thread.updated"
	EventThreadRead       = "thread.read"
	EventJoinRequest      = "join.request"
	EventJoinUpdate       = "join.update"
	EventFriendStatus     = "friend.status"
	EventFriendRequest    = "friend.request"
	EventForumUpdate      = "forum.update"
	EventCheckoutStatus   = "checkout.status"
	EventItineraryUpdated = "itinerary.updated"
	EventItineraryRemoved = "itinerary.removed"
	EventExpenseAdded     = "expense.added"
	EventPhotoAdded       = "photo.added"
	EventTransitDelay     = "transit.delay"
)

// Payload shapes (data of the envelope).
type (
	MessageNewData struct {
		ThreadID string           `json:"thread_id"`
		Message  contract.Message `json:"message"`
	}
	ThreadReadData struct {
		ThreadID string `json:"thread_id"`
	}
	JoinRequestData struct {
		PostID string             `json:"post_id"`
		From   contract.PersonRef `json:"from"`
	}
	JoinUpdateData struct {
		PostID string              `json:"post_id"`
		Result contract.JoinResult `json:"result"`
	}
	FriendStatusData struct {
		UserID     string `json:"user_id"`
		StatusLine string `json:"status_line"`
	}
	CheckoutStatusData struct {
		IntentID string                 `json:"intent_id"`
		State    contract.CheckoutState `json:"state"`
	}
	ItineraryRemovedData struct {
		ItineraryID string `json:"itinerary_id"`
	}
	ExpenseAddedData struct {
		GroupID string           `json:"group_id"`
		Expense contract.Expense `json:"expense"`
	}
	PhotoAddedData struct {
		GroupID string              `json:"group_id"`
		Photo   contract.GroupPhoto `json:"photo"`
	}
	TransitDelayData struct {
		ItineraryID string `json:"itinerary_id"`
		ItemID      string `json:"item_id"`
		Minutes     int    `json:"minutes"`
	}
)

// Typed emitters. Viewer-dependent payloads (message sender_name, thread,
// itinerary) take one recipient; render per recipient and call once each.

// MessageNew tells one thread member about a message rendered for them.
func MessageNew(p Publisher, userID, threadID string, msg contract.Message) {
	p.Send(userID, EventMessageNew, MessageNewData{ThreadID: threadID, Message: msg})
}

// ThreadUpdated sends a thread rendered for one member.
func ThreadUpdated(p Publisher, userID string, thread contract.ChatThread) {
	p.Send(userID, EventThreadUpdated, thread)
}

// ThreadRead tells the reader's other devices a thread was read.
func ThreadRead(p Publisher, userID, threadID string) {
	p.Send(userID, EventThreadRead, ThreadReadData{ThreadID: threadID})
}

// JoinRequest tells a host someone joined their open plan.
func JoinRequest(p Publisher, hostID, postID string, from contract.PersonRef) {
	p.Send(hostID, EventJoinRequest, JoinRequestData{PostID: postID, From: from})
}

// JoinUpdate tells the joiner the result of their join.
func JoinUpdate(p Publisher, userID, postID string, result contract.JoinResult) {
	p.Send(userID, EventJoinUpdate, JoinUpdateData{PostID: postID, Result: result})
}

// FriendStatus tells a user's friends their new status line.
func FriendStatus(p Publisher, friendIDs []string, userID, statusLine string) {
	p.SendMany(friendIDs, EventFriendStatus, FriendStatusData{UserID: userID, StatusLine: statusLine})
}

// FriendRequest delivers an incoming request to its recipient.
func FriendRequest(p Publisher, recipientID string, req contract.FriendRequest) {
	p.Send(recipientID, EventFriendRequest, req)
}

// ForumUpdate asks every client to refetch the feed.
func ForumUpdate(p Publisher) {
	p.Broadcast(EventForumUpdate, nil)
}

// CheckoutStatus tells the intent owner about a state change.
func CheckoutStatus(p Publisher, userID, intentID string, state contract.CheckoutState) {
	p.Send(userID, EventCheckoutStatus, CheckoutStatusData{IntentID: intentID, State: state})
}

// ItineraryUpdated sends an itinerary rendered for one member.
func ItineraryUpdated(p Publisher, userID string, itinerary contract.Itinerary) {
	p.Send(userID, EventItineraryUpdated, itinerary)
}

// ItineraryRemoved tells members (or a leaver) a plan is gone for them.
func ItineraryRemoved(p Publisher, userIDs []string, itineraryID string) {
	p.SendMany(userIDs, EventItineraryRemoved, ItineraryRemovedData{ItineraryID: itineraryID})
}

// ExpenseAdded tells group members about a new expense.
func ExpenseAdded(p Publisher, memberIDs []string, groupID string, expense contract.Expense) {
	p.SendMany(memberIDs, EventExpenseAdded, ExpenseAddedData{GroupID: groupID, Expense: expense})
}

// PhotoAdded tells group members about a new album photo.
func PhotoAdded(p Publisher, memberIDs []string, groupID string, photo contract.GroupPhoto) {
	p.SendMany(memberIDs, EventPhotoAdded, PhotoAddedData{GroupID: groupID, Photo: photo})
}

// TransitDelay tells members a leg is running late.
func TransitDelay(p Publisher, memberIDs []string, itineraryID, itemID string, minutes int) {
	p.SendMany(memberIDs, EventTransitDelay, TransitDelayData{ItineraryID: itineraryID, ItemID: itemID, Minutes: minutes})
}
