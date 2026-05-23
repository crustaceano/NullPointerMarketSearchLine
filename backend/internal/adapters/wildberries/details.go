package wildberries

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

const (
	wildberriesDetailConcurrency            = 3
	wildberriesDetailTimeout                = 5 * time.Second
	wildberriesDetailCharacteristicMaxCount = 18
)

type wbCard struct {
	SubjectName string     `json:"subj_name"`
	Season      string     `json:"season"`
	Contents    string     `json:"contents"`
	Options     []wbOption `json:"options"`
}

type wbOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (a *Wildberries) enrichOffersWithProductDetails(ctx context.Context, offers []models.ProductOffer) {
	shared.EnrichOfferDetails(wildberriesRequestContext(ctx, wildberriesHost+"/"), a.fetcher, offers, shared.DetailEnrichmentConfig{
		Concurrency: wildberriesDetailConcurrency,
		Timeout:     wildberriesDetailTimeout,
		ShouldFetch: shouldFetchWildberriesDetails,
		BuildURL:    wildberriesDetailURL,
		Parse:       parseWildberriesProductDetails,
	})
}

func shouldFetchWildberriesDetails(rawURL string) bool {
	return wildberriesIDFromProductURL(rawURL) > 0
}

func wildberriesDetailURL(offer models.ProductOffer) string {
	id := wildberriesIDFromProductURL(offer.URL)
	if id <= 0 {
		return ""
	}
	return wildberriesCardURL(id)
}

func parseWildberriesProductDetails(page []byte) (map[string]string, error) {
	var card wbCard
	if err := decodeWildberriesJSON(page, &card); err != nil {
		return nil, fmt.Errorf("parse wildberries product details: %w", err)
	}

	chars := map[string]string{}
	if subject := shared.CleanText(card.SubjectName); subject != "" {
		chars["Категория"] = subject
	}
	if season := shared.CleanText(card.Season); season != "" {
		chars["Сезон"] = season
	}
	if contents := shared.CleanText(card.Contents); contents != "" {
		chars["Комплектация"] = limitRunes(contents, 120)
	}

	for _, option := range card.Options {
		if len(chars) >= wildberriesDetailCharacteristicMaxCount {
			break
		}
		name := shared.CleanText(option.Name)
		value := shared.CleanText(option.Value)
		if !isUsefulWildberriesOption(name, value) {
			continue
		}
		if _, exists := chars[name]; exists {
			continue
		}
		chars[name] = limitRunes(value, 160)
	}

	return chars, nil
}

func isUsefulWildberriesOption(name, value string) bool {
	if name == "" || value == "" {
		return false
	}
	lower := strings.ToLower(name)
	skip := []string{
		"артикул",
		"vendor",
		"код",
	}
	for _, marker := range skip {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func limitRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
