package contract

type PastInsight struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Value  string  `json:"value"`
	Detail *string `json:"detail,omitempty"`
	Symbol *string `json:"symbol,omitempty"`
}

type PastInsights struct {
	Headline   string        `json:"headline"`
	Highlights []PastInsight `json:"highlights"`
	TopTags    []string      `json:"top_tags"`
	BasedOn    int           `json:"based_on"`
}

type SearchResults struct {
	Sidequests []Itinerary        `json:"sidequests"`
	People     []UserSearchResult `json:"people"`
	Places     []Place            `json:"places"`
	Posts      []ForumPost        `json:"posts"`
}
