package adapters

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"nullpointer/backend/internal/models"
)

// mockOffers builds a deterministic small set of fake offers for a source.
// Real adapters will replace this with HTTP fetching + HTML parsing.
func mockOffers(source, query, region string, basePrice float64, host string) []models.ProductOffer {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	encoded := url.QueryEscape(q)
	title := strings.Title(q)

	offers := []models.ProductOffer{
		{
			Source:   source,
			Title:    fmt.Sprintf("%s — базовая модель", title),
			Image:    placeholderImage(source, 1),
			Price:    basePrice,
			Currency: "RUB",
			URL:      fmt.Sprintf("https://%s/search?text=%s&region=%s", host, encoded, url.QueryEscape(region)),
			Characteristics: map[string]string{
				"Регион":       region,
				"Гарантия":     "12 мес.",
				"В наличии":    "да",
				"Комплектация": "стандарт",
			},
		},
		{
			Source:   source,
			Title:    fmt.Sprintf("%s — расширенная комплектация", title),
			Image:    placeholderImage(source, 2),
			Price:    basePrice * 1.25,
			Currency: "RUB",
			URL:      fmt.Sprintf("https://%s/search?text=%s&sort=price_desc", host, encoded),
			Characteristics: map[string]string{
				"Регион":       region,
				"Гарантия":     "24 мес.",
				"В наличии":    "да",
				"Комплектация": "расширенная",
			},
		},
		{
			Source:   source,
			Title:    fmt.Sprintf("%s — выгодное предложение", title),
			Image:    placeholderImage(source, 3),
			Price:    basePrice * 0.85,
			Currency: "RUB",
			URL:      fmt.Sprintf("https://%s/search?text=%s&sort=price_asc", host, encoded),
			Characteristics: map[string]string{
				"Регион":    region,
				"Гарантия":  "6 мес.",
				"В наличии": "под заказ",
			},
		},
	}
	return offers
}

func placeholderImage(source string, idx int) string {
	label := html.EscapeString(fmt.Sprintf("%s %d", source, idx))
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="180"><rect width="240" height="180" fill="#e2e8f0"/><text x="120" y="95" text-anchor="middle" font-family="Arial" font-size="18" fill="#475569">%s</text></svg>`, label)
	return "data:image/svg+xml," + url.QueryEscape(svg)
}
