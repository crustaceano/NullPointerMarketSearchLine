package yandex

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

var (
	yandexPriceRe      = regexp.MustCompile(`([0-9][0-9\s]{2,12})\s*₽`)
	yandexRatingRe     = regexp.MustCompile(`(?i)(?:рейтинг|оценка)\s*([1-5][,.]\d)`)
	yandexInlineRateRe = regexp.MustCompile(`(?i)([1-5][,.]\d)\s+[0-9][0-9\s]*\s+(?:отзыв|отзыва|отзывов|оценка|оценки|оценок)`)
	yandexReviewsRe    = regexp.MustCompile(`(?i)(?:^|[^\d,.])([0-9][0-9\s]{0,8})\s+(?:отзыв|отзыва|отзывов|оценка|оценки|оценок)`)
	yandexDeliveryRe   = regexp.MustCompile(`(?i)(доставк[а-яё]*[^.]{0,40}|самовывоз[^.]{0,40}|привез[а-яё]*[^.]{0,40})`)
	unavailableMarkers = []string{
		"сообщить о поступлении",
		"узнать о снижении цены",
		"нет в наличии",
		"нет в продаже",
		"не продается",
		"не продаётся",
		"товар недоступен",
		"товар раскуплен",
		"предложений нет",
		"нет предложений",
		"снят с продажи",
		"когда появится",
		"закончился",
		"распродан",
	}
)

func parseYandexMarketOffers(page []byte, region string) ([]models.ProductOffer, error) {
	apiaryOffers := parseYandexApiaryOffers(page, region)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(page)))
	if err != nil {
		return nil, fmt.Errorf("parse yandex market html: %w", err)
	}

	var offers []models.ProductOffer
	seen := map[string]struct{}{}

	doc.Find(`article[data-auto="searchOrganic"], div[data-zone-name="productSnippet"]`).Each(func(cardIndex int, card *goquery.Selection) {
		if len(offers) >= 12 {
			return
		}

		apiaryOffer := yandexApiaryOfferAt(apiaryOffers, cardIndex)
		cardURL := firstYandexProductURL(card)
		if cardURL == "" {
			cardURL = apiaryOffer.URL
		}
		if cardURL == "" {
			return
		}
		if _, ok := seen[cardURL]; ok {
			return
		}

		if isYandexUnavailableCard(card) {
			return
		}

		title := firstYandexTitle(card)
		if title == "" {
			title = apiaryOffer.Title
		}

		price := firstYandexPrice(card)
		if price <= 0 {
			price = apiaryOffer.Price
		}
		if title == "" || price <= 0 {
			return
		}

		image := firstYandexImage(card)
		if image == "" {
			image = apiaryOffer.Image
		}
		characteristics := shared.MergeCharacteristics(yandexCharacteristics(card, region), apiaryOffer.Characteristics)

		seen[cardURL] = struct{}{}
		offers = append(offers, models.ProductOffer{
			Source:          "Yandex Market",
			Title:           title,
			Image:           image,
			Price:           price,
			Currency:        "RUB",
			URL:             cardURL,
			Characteristics: characteristics,
		})
	})

	offers = appendYandexApiaryFallbacks(offers, apiaryOffers)
	return offers, nil
}

func isYandexUnavailableCard(card *goquery.Selection) bool {
	for _, text := range selectedYandexTexts(card, yandexAvailabilitySelectors()) {
		if hasUnavailableMarker(text) {
			return true
		}
	}

	// Fallback for layout changes: scan the whole card only after selector checks.
	return hasUnavailableMarker(card.Text())
}

func hasUnavailableMarker(text string) bool {
	textLower := strings.ToLower(text)
	for _, marker := range unavailableMarkers {
		if strings.Contains(textLower, marker) {
			return true
		}
	}
	return false
}

func yandexCharacteristics(card *goquery.Selection, region string) map[string]string {
	text := shared.CleanText(card.Text())
	lower := strings.ToLower(text)
	chars := yandexBaseCharacteristics(region)

	if looksAvailableCard(card, lower) {
		chars["В наличии"] = "да"
	}
	if rating := firstYandexRating(card, text); rating != "" {
		chars["Рейтинг"] = rating
	}
	if reviews := firstYandexReviews(card, text); reviews != "" {
		chars["Отзывы"] = reviews
	}
	if delivery := firstYandexDelivery(card, text); delivery != "" {
		chars["Доставка"] = delivery
	}
	return chars
}

func looksAvailableCard(card *goquery.Selection, textLower string) bool {
	for _, text := range selectedYandexTexts(card, yandexAvailabilitySelectors()) {
		if hasAvailableMarker(strings.ToLower(text)) {
			return true
		}
	}
	return hasAvailableMarker(textLower)
}

func hasAvailableMarker(textLower string) bool {
	availableMarkers := []string{
		"в наличии",
		"купить",
		"доставка",
		"самовывоз",
		"завтра",
		"сегодня",
	}
	for _, marker := range availableMarkers {
		if strings.Contains(textLower, marker) {
			return true
		}
	}
	return false
}

func selectedYandexTexts(card *goquery.Selection, selectors []string) []string {
	var out []string
	for _, selector := range selectors {
		card.Find(selector).Each(func(_ int, node *goquery.Selection) {
			if text := shared.CleanText(node.Text()); text != "" {
				out = append(out, text)
			}
			for _, attr := range []string{"aria-label", "title", "data-auto"} {
				if value, ok := node.Attr(attr); ok {
					if cleaned := shared.CleanText(value); cleaned != "" {
						out = append(out, cleaned)
					}
				}
			}
		})
	}
	return out
}

func yandexAvailabilitySelectors() []string {
	return []string{
		`[data-auto*="availability"]`,
		`[data-auto*="stock"]`,
		`[data-auto*="status"]`,
		`[data-auto*="delivery"]`,
		`[data-auto*="cart"]`,
		`[data-auto*="purchase"]`,
		`[data-auto*="notify"]`,
		`[data-auto*="button"]`,
		`button`,
		`[role="button"]`,
		`[aria-label*="налич"]`,
		`[aria-label*="продаж"]`,
		`[title*="налич"]`,
		`[title*="продаж"]`,
	}
}

func firstYandexRating(card *goquery.Selection, text string) string {
	attrSelectors := []string{
		`[data-auto*="rating"]`,
		`[data-auto*="review"]`,
		`[aria-label*="Рейтинг"]`,
		`[aria-label*="рейтинг"]`,
		`[title*="Рейтинг"]`,
		`[title*="рейтинг"]`,
	}
	for _, selector := range attrSelectors {
		node := card.Find(selector).First()
		for _, attr := range []string{"aria-label", "title"} {
			value, ok := node.Attr(attr)
			if ok {
				if rating := extractYandexRating(value); rating != "" {
					return rating
				}
			}
		}
	}
	return extractYandexRating(text)
}

func firstYandexPrice(card *goquery.Selection) float64 {
	selectors := []string{
		`[data-auto="snippet-price-current"]`,
		`[data-auto="snippet-price"]`,
		`[data-auto="price-block"]`,
		`[data-auto="price-value"]`,
		`[data-auto="price"]`,
		`[data-auto*="price"]`,
		`[aria-label*="Цена"]`,
		`[aria-label*="цена"]`,
	}

	for _, selector := range selectors {
		price := extractYandexPrice(card.Find(selector).First().Text())
		if price > 0 {
			return price
		}
	}

	return extractYandexPrice(card.Text())
}

func extractYandexRating(text string) string {
	match := yandexRatingRe.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.ReplaceAll(match[1], ",", ".")
	}
	match = yandexInlineRateRe.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.ReplaceAll(match[1], ",", ".")
	}
	return ""
}

func extractYandexReviews(text string) string {
	match := yandexReviewsRe.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	digits := strings.NewReplacer(" ", "", "\u00a0", "", "\t", "", "\n", "").Replace(match[1])
	if digits == "" {
		return ""
	}
	return digits
}

func firstYandexReviews(card *goquery.Selection, text string) string {
	selectors := []string{
		`[data-auto*="review"]`,
		`[data-auto*="rating"]`,
		`a[href*="reviews"]`,
		`[aria-label*="отзыв"]`,
		`[title*="отзыв"]`,
	}
	for _, candidate := range selectedYandexTexts(card, selectors) {
		if reviews := extractYandexReviews(candidate); reviews != "" {
			return reviews
		}
	}
	return extractYandexReviews(text)
}

func firstYandexDelivery(card *goquery.Selection, text string) string {
	selectors := []string{
		`[data-auto*="delivery"]`,
		`[data-auto*="pickup"]`,
		`[data-auto*="shipment"]`,
		`[aria-label*="достав"]`,
		`[title*="достав"]`,
	}
	for _, candidate := range selectedYandexTexts(card, selectors) {
		if delivery := extractYandexDelivery(candidate); delivery != "" {
			return delivery
		}
	}
	return extractYandexDelivery(text)
}

func extractYandexDelivery(text string) string {
	match := yandexDeliveryRe.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	delivery := shared.CleanText(match[1])
	if len([]rune(delivery)) > 80 {
		return string([]rune(delivery)[:80])
	}
	return delivery
}

func firstYandexProductURL(card *goquery.Selection) string {
	selectors := []string{
		`a[data-auto="snippet-link"]`,
		`a[data-auto="galleryLink"]`,
		`a[href*="/product--"]`,
		`a[href*="/card/"]`,
	}

	for _, selector := range selectors {
		if href, ok := card.Find(selector).First().Attr("href"); ok {
			if out := absoluteYandexURL(href); out != "" {
				return out
			}
		}
	}
	return ""
}

func firstYandexTitle(card *goquery.Selection) string {
	selectors := []string{
		`a[data-auto="snippet-link"]`,
		`h3`,
		`img[alt][src*="get-mpic"]`,
		`img[alt]`,
	}

	for _, selector := range selectors {
		node := card.Find(selector).First()
		title := shared.CleanText(node.Text())
		if title == "" {
			title, _ = node.Attr("alt")
			title = shared.CleanText(title)
		}
		if title != "" {
			return title
		}
	}
	return ""
}

func firstYandexImage(card *goquery.Selection) string {
	img := card.Find(`img[src*="get-mpic"], img[src*="avatars.mds.yandex.net"], img`).First()
	for _, attr := range []string{"src", "data-src"} {
		if value, ok := img.Attr(attr); ok {
			if out := normalizeYandexImageURL(value); out != "" {
				return out
			}
		}
	}
	if srcset, ok := img.Attr("srcset"); ok {
		first := strings.Fields(strings.Split(srcset, ",")[0])
		if len(first) > 0 {
			return normalizeYandexImageURL(first[0])
		}
	}
	return ""
}

func extractYandexPrice(text string) float64 {
	match := yandexPriceRe.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0
	}
	digits := strings.NewReplacer(" ", "", "\u00a0", "", "\t", "", "\n", "").Replace(match[1])
	price, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0
	}
	return price
}
