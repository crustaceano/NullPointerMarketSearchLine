package ozon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"

	"github.com/PuerkitoBio/goquery"
)

type Ozon struct {
	fetcher HTMLFetcher
}

const ozonOfferLimit = 8

func NewOzon(fetcher HTMLFetcher) *Ozon {
	return &Ozon{fetcher: fetcher}
}

func (a *Ozon) Name() string { return "Ozon" }

func (a *Ozon) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	searchURL := buildOzonSearchURL(query, region)

	html, err := a.fetcher.Fetch(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	offers, err := parseOzonOffers(html, region)
	if err != nil {
		return nil, err
	}
	offers = filterOzonOffersForQuery(offers, query)

	log.Printf("Ozon search URL: %s, offers: %d", searchURL, len(offers))

	offers = limitOffers(offers, ozonOfferLimit)
	return offers, nil
}

func buildOzonSearchURL(query, region string) string {
	values := url.Values{}
	values.Set("text", strings.TrimSpace(query))
	values.Set("from_global", "true")
	return "https://www.ozon.ru/search/?" + values.Encode()
}

func parseOzonOffers(html []byte, region string) ([]models.ProductOffer, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse ozon html: %w", err)
	}

	offers := parseOzonStateOffers(doc, region)
	if len(offers) > 0 {
		return offers, nil
	}

	seen := map[string]bool{}
	offers = make([]models.ProductOffer, 0, ozonOfferLimit)

	doc.Find(`a[href*="/product/"]`).EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, ok := s.Attr("href")
		if !ok {
			return true
		}

		productURL := normalizeOzonURL(href)
		if productURL == "" || seen[productURL] {
			return true
		}

		title := strings.TrimSpace(s.Text())
		if title == "" {
			title = nearestText(s)
		}
		title = cleanText(title)

		if !isUsefulOzonTitle(title) {
			return true
		}

		card := nearestCard(s)
		price := extractPrice(card.Text())
		image := extractImage(card)
		if price <= 0 || image == "" {
			return true
		}

		offers = append(offers, models.ProductOffer{
			Source:   "Ozon",
			Title:    title,
			Image:    image,
			Price:    price,
			Currency: "RUB",
			URL:      productURL,
			Characteristics: shared.MergeCharacteristics(
				ozonBaseCharacteristics(region),
				ozonTitleCharacteristics(title),
			),
		})

		seen[productURL] = true
		return len(offers) < ozonOfferLimit
	})

	return offers, nil
}

type ozonTileGridState struct {
	Items []ozonTileItem `json:"items"`
}

type ozonTileItem struct {
	Action    ozonAction      `json:"action"`
	MainState []ozonMainState `json:"mainState"`
	TileImage ozonTileImage   `json:"tileImage"`
}

type ozonAction struct {
	Link string `json:"link"`
}

type ozonMainState struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	TextAtom *ozonTextAtom `json:"textAtom"`
	PriceV2  *ozonPriceV2  `json:"priceV2"`
}

type ozonTextAtom struct {
	Text string `json:"text"`
}

type ozonPriceV2 struct {
	Price []ozonPriceText `json:"price"`
}

type ozonPriceText struct {
	Text      string `json:"text"`
	TextStyle string `json:"textStyle"`
}

type ozonTileImage struct {
	Items []ozonTileImageItem `json:"items"`
}

type ozonTileImageItem struct {
	Image ozonImage `json:"image"`
}

type ozonImage struct {
	Link string `json:"link"`
}

func parseOzonStateOffers(doc *goquery.Document, region string) []models.ProductOffer {
	seen := map[string]bool{}
	offers := make([]models.ProductOffer, 0, ozonOfferLimit)

	doc.Find(`[id^="state-tileGridDesktop-"][data-state]`).EachWithBreak(func(i int, s *goquery.Selection) bool {
		rawState, ok := s.Attr("data-state")
		if !ok {
			return true
		}

		var state ozonTileGridState
		if err := json.Unmarshal([]byte(html.UnescapeString(rawState)), &state); err != nil {
			return true
		}

		for _, item := range state.Items {
			productURL := normalizeOzonURL(item.Action.Link)
			if productURL == "" || seen[productURL] {
				continue
			}

			title := cleanText(extractOzonStateTitle(item.MainState))
			if !isUsefulOzonTitle(title) {
				continue
			}
			price := extractOzonStatePrice(item.MainState)
			image := extractOzonStateImage(item.TileImage)
			if price <= 0 || image == "" {
				continue
			}

			offers = append(offers, models.ProductOffer{
				Source:   "Ozon",
				Title:    title,
				Image:    image,
				Price:    price,
				Currency: "RUB",
				URL:      productURL,
				Characteristics: shared.MergeCharacteristics(
					ozonBaseCharacteristics(region),
					shared.MergeCharacteristics(
						ozonTitleCharacteristics(title),
						extractOzonStateCharacteristics(item.MainState),
					),
				),
			})
			seen[productURL] = true

			if len(offers) >= ozonOfferLimit {
				return false
			}
		}

		return len(offers) < ozonOfferLimit
	})

	return offers
}

func extractOzonStateTitle(states []ozonMainState) string {
	for _, state := range states {
		if state.ID == "name" && state.TextAtom != nil {
			return html.UnescapeString(state.TextAtom.Text)
		}
	}
	for _, state := range states {
		if state.Type == "textAtom" && state.TextAtom != nil {
			return html.UnescapeString(state.TextAtom.Text)
		}
	}
	return ""
}

func extractOzonStatePrice(states []ozonMainState) float64 {
	for _, state := range states {
		if state.PriceV2 == nil {
			continue
		}
		for _, price := range state.PriceV2.Price {
			if price.TextStyle == "PRICE" {
				return extractPrice(price.Text)
			}
		}
		if len(state.PriceV2.Price) > 0 {
			return extractPrice(state.PriceV2.Price[0].Text)
		}
	}
	return 0
}

func extractOzonStateCharacteristics(states []ozonMainState) map[string]string {
	chars := map[string]string{}
	for _, state := range states {
		text := ""
		if state.TextAtom != nil {
			text = cleanText(html.UnescapeString(state.TextAtom.Text))
		}
		if text == "" {
			continue
		}

		switch {
		case state.ID == "brand":
			chars["Бренд"] = text
		case state.ID == "seller":
			chars["Продавец"] = text
		case strings.Contains(strings.ToLower(state.ID), "rating"):
			chars["Рейтинг"] = text
		case strings.Contains(strings.ToLower(state.ID), "delivery"):
			chars["Доставка"] = text
		case strings.Contains(strings.ToLower(text), "в наличии"):
			chars["В наличии"] = "да"
		}
	}
	return chars
}

func filterOzonOffersForQuery(offers []models.ProductOffer, query string) []models.ProductOffer {
	tokens := ozonQueryTokens(query)
	if len(tokens) == 0 || len(offers) == 0 {
		return offers
	}

	filtered := make([]models.ProductOffer, 0, len(offers))
	for _, offer := range offers {
		if ozonOfferMatchesQuery(offer, tokens) {
			filtered = append(filtered, offer)
		}
	}
	return filtered
}

func ozonOfferMatchesQuery(offer models.ProductOffer, tokens []string) bool {
	haystack := strings.ToLower(offer.Title + " " + characteristicsText(offer.Characteristics))
	for _, token := range tokens {
		if strings.Contains(haystack, token) {
			return true
		}
		for _, synonym := range ozonQuerySynonyms(token) {
			if strings.Contains(haystack, synonym) {
				return true
			}
		}
	}
	return false
}

func ozonQueryTokens(query string) []string {
	rawTokens := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'z') && !(r >= 'а' && r <= 'я') && r != 'ё'
	})
	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if len([]rune(token)) < 3 {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func ozonQuerySynonyms(token string) []string {
	switch token {
	case "телефон", "телефоны":
		return []string{"смартфон", "iphone", "айфон", "android"}
	case "кофта", "кофты":
		return []string{"свитшот", "толстовка", "худи", "джемпер", "свитер"}
	default:
		return nil
	}
}

func characteristicsText(chars map[string]string) string {
	if len(chars) == 0 {
		return ""
	}
	parts := make([]string, 0, len(chars)*2)
	for key, value := range chars {
		parts = append(parts, key, value)
	}
	return strings.Join(parts, " ")
}

func isUsefulOzonTitle(title string) bool {
	runeCount := len([]rune(title))
	if runeCount < 5 || runeCount > 180 {
		return false
	}
	lower := strings.ToLower(title)
	badTitles := []string{
		"распродажа",
		"возможно, вам понравится",
		"рекомендуем",
		"похожие товары",
	}
	for _, badTitle := range badTitles {
		if lower == badTitle || strings.HasPrefix(lower, badTitle+" ") {
			return false
		}
	}
	return true
}

func ozonBaseCharacteristics(region string) map[string]string {
	return map[string]string{
		"Регион":    region,
		"Источник":  "Ozon",
		"В наличии": "да",
	}
}

func ozonTitleCharacteristics(title string) map[string]string {
	title = cleanText(title)
	lower := strings.ToLower(title)
	chars := map[string]string{}

	if brand := ozonBrandFromTitle(title); brand != "" {
		chars["Бренд"] = brand
	}
	if ram, storage := ozonMemoryFromTitle(title); ram != "" || storage != "" {
		if ram != "" {
			chars["Оперативная память"] = ram
		}
		if storage != "" {
			chars["Встроенная память"] = storage
		}
	}
	if sim := ozonSIMFromTitle(title); sim != "" {
		chars["SIM-карта"] = sim
	}
	if color := ozonColorFromTitle(lower); color != "" {
		chars["Цвет"] = color
	}
	if cert := ozonCertificationFromTitle(title); cert != "" {
		chars["Сертификация"] = cert
	}

	return chars
}

func ozonBrandFromTitle(title string) string {
	words := strings.Fields(title)
	for _, word := range words {
		cleaned := strings.Trim(word, " ,.;:()[]")
		if cleaned == "" {
			continue
		}
		lower := strings.ToLower(cleaned)
		if isOzonGenericTitleWord(lower) {
			continue
		}
		if strings.Contains(lower, "/") || strings.ContainsAny(lower, "0123456789") {
			continue
		}
		if !isKnownOzonBrand(lower) {
			continue
		}
		return cleaned
	}
	return ""
}

func ozonMemoryFromTitle(title string) (string, string) {
	re := regexp.MustCompile(`(?i)(\d{1,2})\s*/\s*(\d{2,4})\s*(гб|gb|г|g)`)
	match := re.FindStringSubmatch(title)
	if len(match) != 4 {
		return "", ""
	}
	return match[1] + " ГБ", match[2] + " ГБ"
}

func ozonSIMFromTitle(title string) string {
	re := regexp.MustCompile(`(?i)(eSIM(?:\s*\+\s*eSIM)?|Nano-SIM|Micro-SIM|Dual SIM|SIM)`)
	match := re.FindString(title)
	if match == "" {
		return ""
	}
	return cleanText(match)
}

func ozonColorFromTitle(lowerTitle string) string {
	colors := []string{
		"черный",
		"чёрный",
		"белый",
		"серый",
		"синий",
		"голубой",
		"зеленый",
		"зелёный",
		"красный",
		"розовый",
		"фиолетовый",
		"желтый",
		"жёлтый",
		"золотой",
		"серебристый",
		"прозрачный",
	}
	for _, color := range colors {
		if strings.Contains(lowerTitle, color) {
			return color
		}
	}
	return ""
}

func ozonCertificationFromTitle(title string) string {
	lower := strings.ToLower(title)
	if strings.Contains(lower, strings.ToLower("Ростест (EAC)")) {
		return "Ростест (EAC)"
	}

	markers := []string{"Ростест", "EAC", "Global"}
	values := make([]string, 0, len(markers))
	for _, marker := range markers {
		if strings.Contains(lower, strings.ToLower(marker)) && !containsString(values, marker) {
			values = append(values, marker)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ", ")
}

func extractOzonStateImage(tileImage ozonTileImage) string {
	for _, item := range tileImage.Items {
		link := strings.TrimSpace(item.Image.Link)
		if link != "" {
			return link
		}
	}
	return ""
}

func normalizeOzonURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}

	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "/") {
		return "https://www.ozon.ru" + href
	}
	if strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://") {
		return href
	}

	return ""
}

func nearestCard(s *goquery.Selection) *goquery.Selection {
	card := s
	for i := 0; i < 5; i++ {
		parent := card.Parent()
		if parent.Length() == 0 {
			break
		}
		card = parent
	}
	return card
}

func nearestText(s *goquery.Selection) string {
	card := nearestCard(s)
	return cleanText(card.Text())
}

func extractImage(card *goquery.Selection) string {
	image := ""
	card.Find("img").EachWithBreak(func(i int, img *goquery.Selection) bool {
		for _, attr := range []string{"src", "data-src"} {
			value, ok := img.Attr(attr)
			if ok && strings.TrimSpace(value) != "" {
				image = strings.TrimSpace(value)
				return false
			}
		}
		return true
	})
	return image
}

func extractPrice(text string) float64 {
	re := regexp.MustCompile(`[\d\s\x{00A0}\x{2009}\x{202F}]+₽`)
	raw := re.FindString(text)
	if raw == "" {
		return 0
	}
	raw = strings.ReplaceAll(raw, "₽", "")
	raw = regexp.MustCompile(`[^\d]`).ReplaceAllString(raw, "")
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return 0
	}

	price, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return price
}

func cleanText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isOzonGenericTitleWord(word string) bool {
	generic := map[string]struct{}{
		"смартфон":  {},
		"телефон":   {},
		"мобильный": {},
		"ноутбук":   {},
		"планшет":   {},
	}
	_, ok := generic[word]
	return ok
}

func isKnownOzonBrand(word string) bool {
	brands := map[string]struct{}{
		"apple":   {},
		"asus":    {},
		"google":  {},
		"honor":   {},
		"huawei":  {},
		"infinix": {},
		"itel":    {},
		"lenovo":  {},
		"nothing": {},
		"nokia":   {},
		"oneplus": {},
		"oppo":    {},
		"poco":    {},
		"realme":  {},
		"samsung": {},
		"tecno":   {},
		"vivo":    {},
		"xiaomi":  {},
	}
	_, ok := brands[word]
	return ok
}
