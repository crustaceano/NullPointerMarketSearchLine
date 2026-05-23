package yandex

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

const (
	yandexDetailConcurrency            = 3
	yandexDetailTimeout                = 5 * time.Second
	yandexDetailCharacteristicMaxCount = 18
)

func (a *YandexMarket) enrichOffersWithProductDetails(ctx context.Context, offers []models.ProductOffer) {
	shared.EnrichOfferDetails(ctx, a.fetcher, offers, shared.DetailEnrichmentConfig{
		Concurrency: yandexDetailConcurrency,
		Timeout:     yandexDetailTimeout,
		ShouldFetch: shouldFetchYandexDetails,
		Parse:       parseYandexProductDetails,
	})
}

func shouldFetchYandexDetails(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Host != "market.yandex.ru" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/card/") || strings.Contains(parsed.Path, "/product--")
}

func parseYandexProductDetails(page []byte) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(page)))
	if err != nil {
		return nil, fmt.Errorf("parse yandex product html: %w", err)
	}

	chars := map[string]string{}
	specSelectors := []string{
		`[data-auto="product-full-specs"] [data-auto="product-spec"]`,
		`[data-auto="specs-list-fullExtended"] [data-auto="product-spec"]`,
		`[data-auto="specs-list-minimal"] [data-auto="product-spec"]`,
		`[data-zone-name="fullSpecs"] [data-auto="product-spec"]`,
	}

	for _, selector := range specSelectors {
		doc.Find(selector).Each(func(_ int, labelNode *goquery.Selection) {
			if len(chars) >= yandexDetailCharacteristicMaxCount {
				return
			}
			label := shared.CleanText(labelNode.Text())
			if !isUsefulYandexSpecName(label) {
				return
			}

			row := firstYandexSpecRow(labelNode, label)
			if row == nil || row.Length() == 0 {
				return
			}
			value := yandexSpecRowValue(row, label)
			if !isUsefulYandexSpecValue(value) {
				return
			}
			if _, exists := chars[label]; exists {
				return
			}
			chars[label] = value
		})

		if len(chars) > 0 {
			break
		}
	}

	return chars, nil
}

func firstYandexSpecRow(labelNode *goquery.Selection, label string) *goquery.Selection {
	for parent := labelNode.Parent(); parent.Length() > 0; parent = parent.Parent() {
		if parent.Is("body") || parent.Is("html") {
			break
		}
		if parent.Find(`[data-auto="product-spec"]`).Length() != 1 {
			continue
		}

		text := shared.CleanText(parent.Text())
		if text == "" || text == label {
			continue
		}
		if len([]rune(text)) > len([]rune(label))+260 {
			continue
		}
		return parent
	}
	return nil
}

func yandexSpecRowValue(row *goquery.Selection, label string) string {
	clone := row.Clone()
	clone.Find(`[data-auto="product-spec"]`).Remove()
	value := shared.CleanText(clone.Text())
	if value == "" {
		value = strings.TrimSpace(strings.TrimPrefix(shared.CleanText(row.Text()), label))
	}
	return cleanYandexSpecValue(value)
}

func cleanYandexSpecValue(value string) string {
	value = shared.CleanText(value)
	value = strings.TrimPrefix(value, ":")
	value = shared.CleanText(value)

	noise := []string{
		"Скопировать",
		"Подробнее",
	}
	for _, marker := range noise {
		value = strings.TrimSpace(strings.TrimSuffix(value, marker))
	}
	return shared.CleanText(value)
}

func isUsefulYandexSpecName(label string) bool {
	if label == "" {
		return false
	}
	lower := strings.ToLower(label)
	skip := []string{
		"артикул маркета",
		"код товара",
		"модель на маркете",
	}
	for _, marker := range skip {
		if lower == marker {
			return false
		}
	}
	return true
}

func isUsefulYandexSpecValue(value string) bool {
	if value == "" {
		return false
	}
	if len([]rune(value)) > 180 {
		return false
	}
	return true
}
