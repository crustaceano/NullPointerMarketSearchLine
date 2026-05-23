package shared

import (
	"strings"

	"nullpointer/backend/internal/models"
)

func CleanText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func MergeCharacteristics(base, extra map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for key, value := range extra {
		if value == "" {
			continue
		}
		if _, ok := base[key]; !ok {
			base[key] = value
		}
	}
	return base
}

func AbsoluteURL(baseHost, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "/") {
		return baseHost + href
	}
	return baseHost + "/" + href
}

func AbsoluteAssetURL(baseHost, src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "//") {
		return "https:" + src
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return src
	}
	if strings.HasPrefix(src, "/") {
		return baseHost + src
	}
	return src
}

func MaxInt(values ...int) int {
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func LimitOffers(offers []models.ProductOffer, limit int) []models.ProductOffer {
	if limit <= 0 || len(offers) <= limit {
		return offers
	}
	return offers[:limit]
}
