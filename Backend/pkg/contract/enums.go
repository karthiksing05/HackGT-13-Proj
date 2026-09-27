// Package contract mirrors the iOS models (frontend/SideQuestz/Models) field
// for field: snake_case keys, typed enums with Valid(), RFC 3339 UTC times.
// Handlers only ever serialize these types, never Mongo documents.
package contract

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

type AvatarColor string

const (
	AvatarInk    AvatarColor = "ink"
	AvatarSage   AvatarColor = "sage"
	AvatarClay   AvatarColor = "clay"
	AvatarForest AvatarColor = "forest"
	AvatarSand   AvatarColor = "sand"
)

func (c AvatarColor) Valid() bool {
	switch c {
	case AvatarInk, AvatarSage, AvatarClay, AvatarForest, AvatarSand:
		return true
	}
	return false
}

type PresenceStatus string

const (
	StatusOpen        PresenceStatus = "open"
	StatusFriendsOnly PresenceStatus = "friends_only"
	StatusBusy        PresenceStatus = "busy"
)

func (s PresenceStatus) Valid() bool {
	return s == StatusOpen || s == StatusFriendsOnly || s == StatusBusy
}

type AgeBracket string

const (
	AgeUnder13 AgeBracket = "under_13"
	AgeTeen    AgeBracket = "teen"
	AgeUnder21 AgeBracket = "under_21"
	AgeAdult   AgeBracket = "adult"
)

func (a AgeBracket) Valid() bool {
	return a == AgeUnder13 || a == AgeTeen || a == AgeUnder21 || a == AgeAdult
}

type Company string

const (
	CompanySolo       Company = "solo"
	CompanySmallGroup Company = "small_group"
	CompanyBigGroup   Company = "big_group"
)

func (c Company) Valid() bool {
	return c == CompanySolo || c == CompanySmallGroup || c == CompanyBigGroup
}

type Pace string

const (
	PaceRelaxed  Pace = "relaxed"
	PaceBalanced Pace = "balanced"
	PacePacked   Pace = "packed"
)

func (p Pace) Valid() bool { return p == PaceRelaxed || p == PaceBalanced || p == PacePacked }

type SpendTier string

const (
	SpendFreeOnly SpendTier = "free_only"
	SpendUnder15  SpendTier = "under_15"
	Spend15To40   SpendTier = "15_to_40"
	SpendOver40   SpendTier = "over_40"
)

func (s SpendTier) Valid() bool {
	return s == SpendFreeOnly || s == SpendUnder15 || s == Spend15To40 || s == SpendOver40
}

// Budget is the Create-flow budget index this tier maps to (0 Free … 3 $$$).
func (s SpendTier) Budget() int {
	switch s {
	case SpendFreeOnly:
		return 0
	case Spend15To40:
		return 2
	case SpendOver40:
		return 3
	}
	return 1
}

type Flexibility string

const (
	FlexStickToBudget Flexibility = "stick_to_budget"
	FlexBitOverOK     Flexibility = "bit_over_ok"
)

func (f Flexibility) Valid() bool { return f == FlexStickToBudget || f == FlexBitOverOK }

type SplitStyle string

const (
	SplitEqually   SplitStyle = "equally"
	SplitPayOwn    SplitStyle = "pay_own"
	SplitTakeTurns SplitStyle = "take_turns"
)

func (s SplitStyle) Valid() bool {
	return s == SplitEqually || s == SplitPayOwn || s == SplitTakeTurns
}

type BlockKind string

const (
	KindBusy      BlockKind = "busy"
	KindSidequest BlockKind = "sidequest"
	KindTransit   BlockKind = "transit"
	KindGroup     BlockKind = "group"
)

func (k BlockKind) Valid() bool {
	return k == KindBusy || k == KindSidequest || k == KindTransit || k == KindGroup
}

type Visibility string

const (
	VisibilityJustMe  Visibility = "just_me"
	VisibilityFriends Visibility = "friends"
	VisibilityOpen    Visibility = "open"
)

func (v Visibility) Valid() bool {
	return v == VisibilityJustMe || v == VisibilityFriends || v == VisibilityOpen
}

type NotesScope string

const (
	NotesPrivate NotesScope = "private"
	NotesShared  NotesScope = "shared"
)

func (n NotesScope) Valid() bool { return n == NotesPrivate || n == NotesShared }

type TravelMode string

const (
	ModeWalk      TravelMode = "walk"
	ModeMarta     TravelMode = "marta"
	ModeRideshare TravelMode = "rideshare"
	ModeDrive     TravelMode = "drive"
	ModeUber      TravelMode = "uber"
)

func (m TravelMode) Valid() bool {
	switch m {
	case ModeWalk, ModeMarta, ModeRideshare, ModeDrive, ModeUber:
		return true
	}
	return false
}

// Label is the word used in transit item titles ("MARTA to Krog Street Market").
func (m TravelMode) Label() string {
	switch m {
	case ModeWalk:
		return "Walk"
	case ModeMarta:
		return "MARTA"
	case ModeRideshare:
		return "Rideshare"
	case ModeDrive:
		return "Drive"
	case ModeUber:
		return "Uber"
	}
	return string(m)
}

// TravelModes is emitted sorted so sets compare equal on the wire; nil writes [].
type TravelModes []TravelMode

func (m TravelModes) MarshalJSON() ([]byte, error) {
	out := make([]string, 0, len(m))
	for _, mode := range m {
		out = append(out, string(mode))
	}
	slices.Sort(out)
	return json.Marshal(out)
}

// Valid reports whether every mode is known.
func (m TravelModes) Valid() bool {
	for _, mode := range m {
		if !mode.Valid() {
			return false
		}
	}
	return true
}

type TravelRange string

const (
	RangeWalkable TravelRange = "walkable"
	RangeTransit  TravelRange = "transit"
	RangeAnywhere TravelRange = "anywhere"
)

func (r TravelRange) Valid() bool {
	return r == RangeWalkable || r == RangeTransit || r == RangeAnywhere
}

type RideChoice string

const (
	RideDrive RideChoice = "drive"
	RideCover RideChoice = "cover"
	RideNone  RideChoice = "none"
)

func (r RideChoice) Valid() bool { return r == RideDrive || r == RideCover || r == RideNone }

type PlanStopKind string

const (
	StopKindEvent PlanStopKind = "event"
	StopKindPlace PlanStopKind = "place"
)

func (k PlanStopKind) Valid() bool { return k == StopKindEvent || k == StopKindPlace }

type CheckoutState string

const (
	CheckoutPreparing        CheckoutState = "preparing"
	CheckoutAwaitingApproval CheckoutState = "awaiting_approval"
	CheckoutProcessing       CheckoutState = "processing"
	CheckoutBooked           CheckoutState = "booked"
	CheckoutCancelled        CheckoutState = "cancelled"
	CheckoutFailed           CheckoutState = "failed"
)

func (s CheckoutState) Valid() bool {
	switch s {
	case CheckoutPreparing, CheckoutAwaitingApproval, CheckoutProcessing, CheckoutBooked, CheckoutCancelled, CheckoutFailed:
		return true
	}
	return false
}

// CheckoutRunState is where an agentic checkout run is: running until every
// item is booked or failed, then done; cancelled when the buyer stops it.
type CheckoutRunState string

const (
	CheckoutRunRunning   CheckoutRunState = "running"
	CheckoutRunDone      CheckoutRunState = "done"
	CheckoutRunCancelled CheckoutRunState = "cancelled"
)

func (s CheckoutRunState) Valid() bool {
	switch s {
	case CheckoutRunRunning, CheckoutRunDone, CheckoutRunCancelled:
		return true
	}
	return false
}

type ForumPostType string

const (
	PostPlan    ForumPostType = "plan"
	PostFreeNow ForumPostType = "free_now"
)

func (t ForumPostType) Valid() bool { return t == PostPlan || t == PostFreeNow }

type ForumPostVisibility string

const (
	PostFriends  ForumPostVisibility = "friends"
	PostEveryone ForumPostVisibility = "everyone"
)

func (v ForumPostVisibility) Valid() bool { return v == PostFriends || v == PostEveryone }

type JoinStatus string

const (
	JoinNone      JoinStatus = "none"
	JoinRequested JoinStatus = "requested"
	JoinJoined    JoinStatus = "joined"
	JoinFull      JoinStatus = "full"
	JoinClosed    JoinStatus = "closed"
)

func (s JoinStatus) Valid() bool {
	switch s {
	case JoinNone, JoinRequested, JoinJoined, JoinFull, JoinClosed:
		return true
	}
	return false
}

type FriendActivity string

const (
	ActivityFree        FriendActivity = "free"
	ActivityOnSidequest FriendActivity = "on_sidequest"
	ActivityBusy        FriendActivity = "busy"
	ActivityNew         FriendActivity = "new"
)

func (a FriendActivity) Valid() bool {
	return a == ActivityFree || a == ActivityOnSidequest || a == ActivityBusy || a == ActivityNew
}

type FriendRelation string

const (
	RelationNone     FriendRelation = "none"
	RelationFriend   FriendRelation = "friend"
	RelationOutgoing FriendRelation = "outgoing"
	RelationIncoming FriendRelation = "incoming"
)

func (r FriendRelation) Valid() bool {
	return r == RelationNone || r == RelationFriend || r == RelationOutgoing || r == RelationIncoming
}

type CalendarProvider string

const (
	ProviderGoogle  CalendarProvider = "google"
	ProviderOutlook CalendarProvider = "outlook"
)

func (p CalendarProvider) Valid() bool { return p == ProviderGoogle || p == ProviderOutlook }

// TripTypes are the canonical rating keys (Setup › "What do you enjoy?").
var TripTypes = []string{"outdoors", "food", "museums", "live_music", "nightlife", "sports", "shopping", "big_crowds", "early_mornings", "long_walks"}

// AnswerKeys are the canonical open-answer keys (Setup step 5).
var AnswerKeys = []string{"perfect_afternoon", "never_do", "plan_around"}

// IsTripType reports whether k (already canonical) is a rating key.
func IsTripType(k string) bool { return slices.Contains(TripTypes, k) }

// CanonicalKey converts camelCase or PascalCase ("liveMusic", "BigCrowds") to
// snake_case ("live_music"); snake_case and lowercase keys pass through.
func CanonicalKey(k string) string {
	k = strings.TrimSpace(k)
	var b strings.Builder
	b.Grow(len(k) + 4)
	prevLower := false
	for _, r := range k {
		if unicode.IsUpper(r) {
			if prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			prevLower = false
			continue
		}
		b.WriteRune(r)
		prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return b.String()
}

// Ratings maps trip types to 1–5. Keys are canonical snake_case on the wire;
// camelCase input is accepted. A nil map writes {}.
type Ratings map[string]int

func (r Ratings) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]int(r))
}

func (r *Ratings) UnmarshalJSON(b []byte) error {
	var raw map[string]int
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == nil {
		*r = nil
		return nil
	}
	out := make(Ratings, len(raw))
	for k, v := range raw {
		out[CanonicalKey(k)] = v
	}
	*r = out
	return nil
}

// Validate checks that every key is a trip type and every value 1–5.
func (r Ratings) Validate() bool {
	for k, v := range r {
		if !IsTripType(k) || v < 1 || v > 5 {
			return false
		}
	}
	return true
}

// Answers holds the open-ended setup answers keyed perfect_afternoon,
// never_do and plan_around (camelCase accepted). A nil map writes {}.
type Answers map[string]string

func (a Answers) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]string(a))
}

func (a *Answers) UnmarshalJSON(b []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == nil {
		*a = nil
		return nil
	}
	out := make(Answers, len(raw))
	for k, v := range raw {
		out[CanonicalKey(k)] = v
	}
	*a = out
	return nil
}
