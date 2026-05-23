package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"nullpointer/backend/internal/adapters"
	"nullpointer/backend/internal/models"
	"nullpointer/backend/internal/normalizer"
)

type SearchHandler struct {
	Adapters []adapters.SourceAdapter
	ML       *normalizer.Client
}

func NewSearchHandler(ads []adapters.SourceAdapter, ml *normalizer.Client) *SearchHandler {
	return &SearchHandler{Adapters: ads, ML: ml}
}

func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query, region, err := parseSearchRequest(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	norm := h.ML.Normalize(ctx, query)
	searchQuery := norm.Corrected
	if searchQuery == "" {
		searchQuery = query
	}

	sources := h.fanOut(ctx, searchQuery, region, norm)

	resp := models.SearchResponse{
		Query:         query,
		Region:        region,
		Normalization: norm,
		Sources:       sources,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// fanOut queries all adapters in parallel and collects per-source results.
// A failure in one adapter never breaks the response from the others.
func (h *SearchHandler) fanOut(ctx context.Context, query, region string, norm models.Normalization) []models.SourceResult {
	results := make([]models.SourceResult, len(h.Adapters))
	var wg sync.WaitGroup

	for i, a := range h.Adapters {
		wg.Add(1)
		go func(i int, a adapters.SourceAdapter) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					results[i] = models.SourceResult{
						Source: a.Name(),
						Status: models.StatusError,
						Error:  "internal adapter panic",
						Offers: []models.ProductOffer{},
					}
				}
			}()

			offers, err := a.Search(ctx, query, region)
			if err == nil {
				offers = filterRelevantOffers(offers, norm)
			}
			res := models.SourceResult{Source: a.Name(), Offers: []models.ProductOffer{}}
			switch {
			case err != nil:
				res.Status = models.StatusError
				res.Error = err.Error()
			case len(offers) == 0:
				res.Status = models.StatusEmpty
			default:
				res.Status = models.StatusSuccess
				res.Offers = offers
			}
			results[i] = res
		}(i, a)
	}

	wg.Wait()
	return results
}

type searchRequestBody struct {
	Query  string `json:"query"`
	Region string `json:"region"`
}

func parseSearchRequest(r *http.Request) (query, region string, err error) {
	if r.Method == http.MethodPost {
		var body searchRequestBody
		if decErr := json.NewDecoder(r.Body).Decode(&body); decErr == nil {
			query = body.Query
			region = body.Region
		}
	}
	if query == "" {
		query = r.URL.Query().Get("query")
	}
	if region == "" {
		region = r.URL.Query().Get("region")
	}
	query = strings.TrimSpace(query)
	region = strings.TrimSpace(region)
	if query == "" {
		return "", "", errMissingQuery
	}
	if region == "" {
		region = "Москва"
	}
	return query, region, nil
}

type httpError struct{ msg string }

func (e *httpError) Error() string { return e.msg }

var errMissingQuery = &httpError{msg: "query is required"}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
