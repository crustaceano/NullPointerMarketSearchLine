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
	searchURL := buildOzonSearchURL(query)

	html, err := a.fetcher.Fetch(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	offers, err := parseOzonOffers(html, region)
	if err != nil {
		return nil, err
	}

	log.Printf("Ozon search URL: %s, offers: %d", searchURL, len(offers))

	return limitOffers(offers, ozonOfferLimit), nil
}

func buildOzonSearchURL(query string) string {
	q := url.QueryEscape(strings.TrimSpace(query))
	return "https://www.ozon.ru/search/?text=" + q
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

		if len([]rune(title)) < 5 {
			return true
		}

		card := nearestCard(s)
		price := extractPrice(card.Text())
		image := extractImage(card)

		offers = append(offers, models.ProductOffer{
			Source:   "Ozon",
			Title:    title,
			Image:    image,
			Price:    price,
			Currency: "RUB",
			URL:      productURL,
			Characteristics: map[string]string{
				"Регион": region,
			},
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
			if len([]rune(title)) < 5 {
				continue
			}

			offers = append(offers, models.ProductOffer{
				Source:   "Ozon",
				Title:    title,
				Image:    extractOzonStateImage(item.TileImage),
				Price:    extractOzonStatePrice(item.MainState),
				Currency: "RUB",
				URL:      productURL,
				Characteristics: map[string]string{
					"Регион": region,
				},
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
