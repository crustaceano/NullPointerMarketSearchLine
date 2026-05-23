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
		// /expand тяжелее /normalize (lazy-load WordNet на первом запросе),
		// поэтому таймаут чуть щедрее.
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// expandRequest matches POST /expand on the ML service.
type expandRequest struct {
	Query      string `json:"query"`
	MaxQueries int    `json:"max_queries"`
}

// expandableToken / expandedQuery / expandResponse mirror the /expand
// schema. Поля, которые backend сейчас не использует, опущены.
type expandableToken struct {
	Text     string   `json:"text"`
	Synonyms []string `json:"synonyms"`
}

type expandedQuery struct {
	Query string `json:"query"`
	Valid bool   `json:"valid"`
}

type expandResponse struct {
	Raw              string            `json:"raw"`
	Corrected        string            `json:"corrected"`
	TypoCorrected    *string           `json:"typo_corrected"`
	ExpandableTokens []expandableToken `json:"expandable_tokens"`
	ExpandedQueries  []expandedQuery   `json:"expanded_queries"`
}

// Normalize calls /expand on the ML service and adapts the response to the
// legacy `models.Normalization` contract used by the frontend. On any
// failure returns a safe fallback so /search keeps working.
func (c *Client) Normalize(ctx context.Context, query string) models.Normalization {
	fallback := models.Normalization{
		Raw:             query,
		Corrected:       query,
		Synonyms:        []string{},
		ExpandedQueries: []string{query},
	}

	body, err := json.Marshal(expandRequest{Query: query, MaxQueries: 10})
	if err != nil {
		return fallback
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/expand", bytes.NewReader(body))
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

	var raw expandResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fallback
	}

	out := models.Normalization{
		Raw:       raw.Raw,
		Corrected: raw.Corrected,
		// `corrected` тут — raw-ветка (без SymSpell). Для UI логичнее
		// показать SymSpell-исправленный вариант, если он есть.
		Synonyms:        flattenSynonyms(raw.ExpandableTokens, 10),
		ExpandedQueries: collectValidQueries(raw, 10, query),
	}
	if raw.TypoCorrected != nil && *raw.TypoCorrected != "" && *raw.TypoCorrected != raw.Corrected {
		out.Corrected = *raw.TypoCorrected
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

// flattenSynonyms собирает уникальный плоский список синонимов поверх
// всех expandable-токенов (топ-N для UI-чипа).
func flattenSynonyms(tokens []expandableToken, limit int) []string {
	seen := make(map[string]struct{}, limit)
	out := make([]string, 0, limit)
	for _, t := range tokens {
		for _, s := range t.Synonyms {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// collectValidQueries берёт valid expanded queries в их естественном
// порядке (raw-ветка → typo_corrected). Дедуп уже сделан на ML-стороне,
// но повторим тут для надёжности.
func collectValidQueries(r expandResponse, limit int, fallback string) []string {
	seen := make(map[string]struct{}, limit)
	out := make([]string, 0, limit)
	for _, q := range r.ExpandedQueries {
		if !q.Valid || q.Query == "" {
			continue
		}
		if _, ok := seen[q.Query]; ok {
			continue
		}
		seen[q.Query] = struct{}{}
		out = append(out, q.Query)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 && fallback != "" {
		out = append(out, fallback)
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
