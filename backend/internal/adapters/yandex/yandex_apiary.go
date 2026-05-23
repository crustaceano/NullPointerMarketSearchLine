package yandex

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

var (
	yandexApiaryRe     = regexp.MustCompile(`(?s)<noframes data-apiary="patch">(.*?)</noframes>`)
	yandexSerpEntityRe = regexp.MustCompile(`serpEntity[0-9]+`)
)

type yandexApiaryPatch struct {
	Widgets map[string]json.RawMessage `json:"widgets"`
}

type yandexApiaryOfferBuilder struct {
	offer       models.ProductOffer
	quantityMax int
	unavailable bool
}

type yandexAddToCartPatch struct {
	PendingCartItem *yandexPendingCartItem `json:"pendingCartItem"`
	DisableAddItem  bool                   `json:"disableAddItem"`
	IsError         bool                   `json:"isError"`
	Quantity        yandexCartQuantity     `json:"quantity"`
	QuantityMaximum int                    `json:"quantityMaximum"`
}

type yandexPendingCartItem struct {
	ProductID int64              `json:"productId"`
	SKUID     int64              `json:"skuId"`
	Price     yandexApiaryPrice  `json:"price"`
	Name      string             `json:"name"`
	Count     int                `json:"count"`
	ImageMeta yandexApiaryImage  `json:"imageMeta"`
	Quantity  yandexCartQuantity `json:"quantity"`
}

type yandexApiaryPrice struct {
	Value    int64  `json:"value"`
	Currency string `json:"currency"`
	ValueFmt string `json:"valueFmt"`
}

type yandexApiaryImage struct {
	Key       string `json:"key"`
	Namespace string `json:"namespace"`
	GroupID   int64  `json:"groupId"`
}

type yandexCartQuantity struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
}

type yandexImageManagerPatch struct {
	PropsToCreateImages struct {
		Pictures []struct {
			Picture struct {
				BaseURL string `json:"baseUrl"`
			} `json:"picture"`
		} `json:"pictures"`
	} `json:"propsToCreateImages"`
}

func parseYandexApiaryOffers(page []byte, region string) []models.ProductOffer {
	matches := yandexApiaryRe.FindAllSubmatch(page, -1)
	if len(matches) == 0 {
		return nil
	}

	byEntity := map[string]*yandexApiaryOfferBuilder{}
	var order []string

	ensureBuilder := func(entity string) *yandexApiaryOfferBuilder {
		builder, ok := byEntity[entity]
		if ok {
			return builder
		}
		builder = &yandexApiaryOfferBuilder{
			offer: models.ProductOffer{
				Source:          "Yandex Market",
				Currency:        "RUB",
				Characteristics: yandexBaseCharacteristics(region),
			},
		}
		byEntity[entity] = builder
		order = append(order, entity)
		return builder
	}

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		var patch yandexApiaryPatch
		rawJSON := html.UnescapeString(string(match[1]))
		if err := json.Unmarshal([]byte(rawJSON), &patch); err != nil {
			continue
		}

		for widgetName, widgetRaw := range patch.Widgets {
			var byPath map[string]json.RawMessage
			if err := json.Unmarshal(widgetRaw, &byPath); err != nil {
				continue
			}

			switch {
			case strings.Contains(widgetName, "AddToCartButton"):
				for path, payload := range byPath {
					entity := yandexEntityKey(path)
					if entity == "" {
						continue
					}
					applyYandexApiaryCart(ensureBuilder(entity), payload, region)
				}
			case strings.Contains(widgetName, "ImageManager"):
				for path, payload := range byPath {
					entity := yandexEntityKey(path)
					if entity == "" {
						continue
					}
					applyYandexApiaryImage(ensureBuilder(entity), payload)
				}
			}
		}
	}

	offers := make([]models.ProductOffer, 0, len(order))
	for _, entity := range order {
		builder := byEntity[entity]
		if builder == nil || builder.unavailable {
			continue
		}
		if builder.offer.Title == "" || builder.offer.Price <= 0 {
			continue
		}
		if builder.offer.URL == "" {
			builder.offer.URL = yandexSearchURL(builder.offer.Title)
		}
		if builder.quantityMax > 0 {
			builder.offer.Characteristics["В наличии"] = "да"
			builder.offer.Characteristics["Остаток"] = strconv.Itoa(builder.quantityMax)
		}
		offers = append(offers, builder.offer)
	}

	return offers
}

func applyYandexApiaryCart(builder *yandexApiaryOfferBuilder, payload json.RawMessage, region string) {
	var cart yandexAddToCartPatch
	if err := json.Unmarshal(payload, &cart); err != nil {
		return
	}
	if cart.IsError || cart.DisableAddItem {
		builder.unavailable = true
		return
	}
	if cart.PendingCartItem == nil {
		return
	}

	item := cart.PendingCartItem
	if title := shared.CleanText(item.Name); title != "" {
		builder.offer.Title = title
	}
	if price := parseYandexApiaryPrice(item.Price); price > 0 {
		builder.offer.Price = price
	}
	builder.offer.Currency = normalizeYandexCurrency(item.Price.Currency)
	if image := yandexImageFromMeta(item.ImageMeta); image != "" && builder.offer.Image == "" {
		builder.offer.Image = image
	}
	if url := yandexApiaryProductURL(item); url != "" {
		builder.offer.URL = url
	}
	if builder.offer.Characteristics == nil {
		builder.offer.Characteristics = yandexBaseCharacteristics(region)
	}

	builder.quantityMax = shared.MaxInt(builder.quantityMax, item.Count, item.Quantity.Maximum, cart.Quantity.Maximum, cart.QuantityMaximum)
}

func applyYandexApiaryImage(builder *yandexApiaryOfferBuilder, payload json.RawMessage) {
	var imageManager yandexImageManagerPatch
	if err := json.Unmarshal(payload, &imageManager); err != nil {
		return
	}
	if builder.offer.Image != "" {
		return
	}
	for _, picture := range imageManager.PropsToCreateImages.Pictures {
		if image := normalizeYandexImageURL(picture.Picture.BaseURL); image != "" {
			builder.offer.Image = image
			return
		}
	}
}

func yandexApiaryOfferAt(offers []models.ProductOffer, index int) models.ProductOffer {
	if index < 0 || index >= len(offers) {
		return models.ProductOffer{}
	}
	return offers[index]
}

func appendYandexApiaryFallbacks(offers, apiaryOffers []models.ProductOffer) []models.ProductOffer {
	if len(apiaryOffers) == 0 || len(offers) >= 12 {
		return offers
	}

	seenURLs := map[string]struct{}{}
	seenTitles := map[string]struct{}{}
	for _, offer := range offers {
		if offer.URL != "" {
			seenURLs[offer.URL] = struct{}{}
		}
		if key := yandexOfferTitleKey(offer.Title); key != "" {
			seenTitles[key] = struct{}{}
		}
	}

	for _, offer := range apiaryOffers {
		if len(offers) >= 12 {
			break
		}
		if offer.Title == "" || offer.Price <= 0 || offer.URL == "" {
			continue
		}
		if _, ok := seenURLs[offer.URL]; ok {
			continue
		}
		titleKey := yandexOfferTitleKey(offer.Title)
		if _, ok := seenTitles[titleKey]; ok {
			continue
		}

		offers = append(offers, offer)
		seenURLs[offer.URL] = struct{}{}
		seenTitles[titleKey] = struct{}{}
	}

	return offers
}

func yandexEntityKey(path string) string {
	return yandexSerpEntityRe.FindString(path)
}

func yandexOfferTitleKey(title string) string {
	return strings.ToLower(shared.CleanText(title))
}

func parseYandexApiaryPrice(price yandexApiaryPrice) float64 {
	if price.ValueFmt != "" {
		normalized := strings.NewReplacer(" ", "", "\u00a0", "", ",", ".").Replace(price.ValueFmt)
		value, err := strconv.ParseFloat(normalized, 64)
		if err == nil {
			return value
		}
	}
	if price.Value <= 0 {
		return 0
	}
	return float64(price.Value) / 10000000
}

func normalizeYandexCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" || currency == "RUR" {
		return "RUB"
	}
	return currency
}

func yandexImageFromMeta(image yandexApiaryImage) string {
	if image.GroupID == 0 || image.Key == "" {
		return ""
	}
	namespace := strings.TrimPrefix(image.Namespace, "get-")
	if namespace == "" {
		namespace = "mpic"
	}
	return fmt.Sprintf("https://avatars.mds.yandex.net/get-%s/%d/%s/orig", namespace, image.GroupID, image.Key)
}

func yandexApiaryProductURL(item *yandexPendingCartItem) string {
	if item == nil || item.Name == "" {
		return ""
	}
	// Apiary cart payload does not expose a stable human product link, so this is
	// only a valid fallback when the DOM card link is missing.
	return yandexSearchURL(item.Name)
}
