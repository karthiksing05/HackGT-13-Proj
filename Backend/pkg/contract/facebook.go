package contract

type FacebookImport struct {
	ImportedAt       Time               `json:"imported_at"`
	LikedPages       int                `json:"liked_pages"`
	SuggestedRatings Ratings            `json:"suggested_ratings"`
	Interests        []string           `json:"interests"`
	HomeArea         *string            `json:"home_area,omitempty"`
	FriendsOnApp     []UserSearchResult `json:"friends_on_app"`
}

type FacebookConnection struct {
	Connected      bool            `json:"connected"`
	NeedsReconnect bool            `json:"needs_reconnect"`
	Name           *string         `json:"name,omitempty"`
	DeclinedScopes []string        `json:"declined_scopes"`
	LastImport     *FacebookImport `json:"last_import,omitempty"`
}

type FacebookConnectRequest struct {
	Rerequest bool `json:"rerequest"`
}

type DataDeletionResponse struct {
	URL              string `json:"url"`
	ConfirmationCode string `json:"confirmation_code"`
}
