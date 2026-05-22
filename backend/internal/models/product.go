package models

// ProductOffer is a unified representation of a product offer
// returned by any source adapter.
type ProductOffer struct {
	Source          string            `json:"source"`
	Title           string            `json:"title"`
	Image           string            `json:"image"`
	Price           float64           `json:"price"`
	Currency        string            `json:"currency"`
	URL             string            `json:"url"`
	Characteristics map[string]string `json:"characteristics"`
}

// SourceStatus describes the outcome of querying one source.
type SourceStatus string

const (
	StatusSuccess SourceStatus = "success"
	StatusError   SourceStatus = "error"
	StatusEmpty   SourceStatus = "empty"
)

// SourceResult is the grouped result for a single source.
type SourceResult struct {
	Source string         `json:"source"`
	Status SourceStatus   `json:"status"`
	Error  string         `json:"error,omitempty"`
	Offers []ProductOffer `json:"offers"`
}

// Normalization is what the Python ML service returns for a raw query.
type Normalization struct {
	Raw             string   `json:"raw"`
	Corrected       string   `json:"corrected"`
	Synonyms        []string `json:"synonyms"`
	ExpandedQueries []string `json:"expanded_queries"`
}

// SearchResponse is the full response of the /search endpoint.
type SearchResponse struct {
	Query         string         `json:"query"`
	Region        string         `json:"region"`
	Normalization Normalization  `json:"normalization"`
	Sources       []SourceResult `json:"sources"`
}
