package normalizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"nullpointer/backend/internal/models"
)

// Client talks to the Python ML normalization service over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a normalizer client pointed at baseURL (e.g. http://localhost:8000).
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

type normalizeRequest struct {
	Query string `json:"query"`
}

// Normalize calls the ML service. On any failure it returns a safe
// fallback (the raw query is reused everywhere) so /search keeps working.
func (c *Client) Normalize(ctx context.Context, query string) models.Normalization {
	fallback := models.Normalization{
		Raw:             query,
		Corrected:       query,
		Synonyms:        []string{},
		ExpandedQueries: []string{query},
	}

	body, err := json.Marshal(normalizeRequest{Query: query})
	if err != nil {
		return fallback
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/normalize", bytes.NewReader(body))
	if err != nil {
		return fallback
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallback
	}

	var out models.Normalization
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fallback
	}
	if out.Raw == "" {
		out.Raw = query
	}
	if out.Corrected == "" {
		out.Corrected = query
	}
	if len(out.ExpandedQueries) == 0 {
		out.ExpandedQueries = []string{out.Corrected}
	}
	if out.Synonyms == nil {
		out.Synonyms = []string{}
	}
	return out
}

// Ping returns nil if the ML service responds healthily.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ml service status %d", resp.StatusCode)
	}
	return nil
}
