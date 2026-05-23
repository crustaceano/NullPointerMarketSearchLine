package ozon

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

const (
	ozonDetailConcurrency            = 2
	ozonDetailTimeout                = 7 * time.Second
	ozonDetailCharacteristicMaxCount = 18
)

var ozonDetailDumpOnce sync.Once

func (a *Ozon) enrichOffersWithProductDetails(ctx context.Context, offers []models.ProductOffer) {
	shared.EnrichOfferDetails(ctx, a.fetcher, offers, shared.DetailEnrichmentConfig{
		Concurrency: ozonDetailConcurrency,
		Timeout:     ozonDetailTimeout,
		ShouldFetch: shouldFetchOzonDetails,
		Parse:       parseOzonProductDetails,
		OnResult:    logOzonDetailEnrichmentResult,
	})
}

func logOzonDetailEnrichmentResult(result shared.DetailEnrichmentResult) {
	switch {
	case result.FetchErr != nil:
		log.Printf("[Ozon details] fetch failed url=%s err=%v", result.URL, result.FetchErr)
	case result.ParseErr != nil:
		log.Printf("[Ozon details] parse failed url=%s bytes=%d err=%v", result.URL, result.PageBytes, result.ParseErr)
	case result.Characteristic == 0:
		log.Printf("[Ozon details] no characteristics url=%s bytes=%d", result.URL, result.PageBytes)
		dumpOzonDetailHTML(result)
	default:
		log.Printf("[Ozon details] enriched url=%s bytes=%d characteristics=%d", result.URL, result.PageBytes, result.Characteristic)
	}
}

func dumpOzonDetailHTML(result shared.DetailEnrichmentResult) {
	if len(result.Page) == 0 {
		return
	}

	ozonDetailDumpOnce.Do(func() {
		path := filepath.Join(os.TempDir(), "ozon-detail-debug.html")
		if err := os.WriteFile(path, result.Page, 0o644); err != nil {
			log.Printf("[Ozon details] debug dump failed path=%s err=%v", path, err)
			return
		}
		log.Printf("[Ozon details] debug dump saved path=%s url=%s bytes=%d", path, result.URL, len(result.Page))
	})
}

func shouldFetchOzonDetails(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(parsed.Host, "ozon.ru") && strings.HasPrefix(parsed.Path, "/product/")
}

func parseOzonProductDetails(page []byte) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(page)))
	if err != nil {
		return nil, fmt.Errorf("parse ozon product html: %w", err)
	}

	chars := map[string]string{}
	parseOzonDefinitionLists(doc, chars)
	parseOzonTableRows(doc, chars)
	parseOzonCharacteristicWidget(doc, chars)
	parseOzonDataStateCharacteristics(doc, chars)
	return chars, nil
}

func parseOzonDefinitionLists(doc *goquery.Document, chars map[string]string) {
	doc.Find("dl").EachWithBreak(func(_ int, list *goquery.Selection) bool {
		var lastName string
		list.ChildrenFiltered("dt,dd").Each(func(_ int, node *goquery.Selection) {
			if len(chars) >= ozonDetailCharacteristicMaxCount {
				return
			}
			switch goquery.NodeName(node) {
			case "dt":
				lastName = shared.CleanText(node.Text())
			case "dd":
				addOzonCharacteristic(chars, lastName, node.Text())
				lastName = ""
			}
		})
		return len(chars) < ozonDetailCharacteristicMaxCount
	})
}

func parseOzonTableRows(doc *goquery.Document, chars map[string]string) {
	doc.Find("tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		if len(chars) >= ozonDetailCharacteristicMaxCount {
			return false
		}
		name := shared.CleanText(row.Find("th").First().Text())
		value := shared.CleanText(row.Find("td").First().Text())
		if name == "" || value == "" {
			cells := row.Find("td")
			name = shared.CleanText(cells.Eq(0).Text())
			value = shared.CleanText(cells.Eq(1).Text())
		}
		addOzonCharacteristic(chars, name, value)
		return true
	})
}

func parseOzonCharacteristicWidget(doc *goquery.Document, chars map[string]string) {
	selectors := []string{
		`[data-widget="webCharacteristics"]`,
		`[data-widget="webShortCharacteristics"]`,
		`[id*="characteristics"]`,
	}

	for _, selector := range selectors {
		doc.Find(selector).EachWithBreak(func(_ int, root *goquery.Selection) bool {
			root.Find("div, li").EachWithBreak(func(_ int, row *goquery.Selection) bool {
				if len(chars) >= ozonDetailCharacteristicMaxCount {
					return false
				}
				name, value := splitOzonCharacteristicText(row.Text())
				addOzonCharacteristic(chars, name, value)
				return true
			})
			return len(chars) < ozonDetailCharacteristicMaxCount
		})
	}
}

func parseOzonDataStateCharacteristics(doc *goquery.Document, chars map[string]string) {
	doc.Find("[data-state]").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if len(chars) >= ozonDetailCharacteristicMaxCount {
			return false
		}

		rawState, ok := node.Attr("data-state")
		if !ok {
			return true
		}
		rawState = strings.TrimSpace(html.UnescapeString(rawState))
		if rawState == "" || (!strings.HasPrefix(rawState, "{") && !strings.HasPrefix(rawState, "[")) {
			return true
		}

		var state any
		if err := json.Unmarshal([]byte(rawState), &state); err != nil {
			return true
		}
		collectOzonJSONCharacteristics(state, chars)
		return len(chars) < ozonDetailCharacteristicMaxCount
	})
}

func collectOzonJSONCharacteristics(value any, chars map[string]string) {
	if len(chars) >= ozonDetailCharacteristicMaxCount {
		return
	}

	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectOzonJSONCharacteristics(item, chars)
			if len(chars) >= ozonDetailCharacteristicMaxCount {
				return
			}
		}
	case map[string]any:
		if name, val := ozonJSONCharacteristicPair(typed); name != "" && val != "" {
			addOzonCharacteristic(chars, name, val)
		}
		for _, child := range typed {
			collectOzonJSONCharacteristics(child, chars)
			if len(chars) >= ozonDetailCharacteristicMaxCount {
				return
			}
		}
	}
}

func ozonJSONCharacteristicPair(obj map[string]any) (string, string) {
	name := firstOzonJSONText(obj, "name", "title", "key", "label", "caption")
	if name == "" {
		return "", ""
	}

	value := firstOzonJSONText(obj, "value", "values", "text", "description")
	if value == "" || strings.EqualFold(name, value) {
		return "", ""
	}
	return name, value
}

func firstOzonJSONText(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			if text := ozonJSONText(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func ozonJSONText(value any) string {
	switch typed := value.(type) {
	case string:
		return shared.CleanText(html.UnescapeString(typed))
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "да"
		}
		return "нет"
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := ozonJSONText(item)
			if text == "" {
				continue
			}
			parts = append(parts, text)
			if len(parts) >= 6 {
				break
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return firstOzonJSONText(typed, "text", "value", "name", "title")
	default:
		return ""
	}
}

func splitOzonCharacteristicText(text string) (string, string) {
	text = shared.CleanText(text)
	for _, sep := range []string{":", " — ", " - "} {
		before, after, ok := strings.Cut(text, sep)
		if ok {
			return before, after
		}
	}
	return "", ""
}

func addOzonCharacteristic(chars map[string]string, name, value string) {
	if len(chars) >= ozonDetailCharacteristicMaxCount {
		return
	}
	name = cleanOzonCharacteristicName(name)
	value = cleanOzonCharacteristicValue(value)
	if !isUsefulOzonCharacteristic(name, value) {
		return
	}
	if _, exists := chars[name]; exists {
		return
	}
	chars[name] = limitOzonRunes(value, 160)
}

func cleanOzonCharacteristicName(name string) string {
	name = shared.CleanText(name)
	name = strings.TrimSuffix(name, ":")
	return shared.CleanText(name)
}

func cleanOzonCharacteristicValue(value string) string {
	value = shared.CleanText(value)
	value = strings.TrimPrefix(value, ":")
	return shared.CleanText(value)
}

func isUsefulOzonCharacteristic(name, value string) bool {
	if name == "" || value == "" {
		return false
	}
	if len([]rune(name)) > 80 || len([]rune(value)) > 220 {
		return false
	}
	lower := strings.ToLower(name)
	skip := []string{
		"артикул",
		"код товара",
		"sku",
		"id товара",
	}
	for _, marker := range skip {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func limitOzonRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
