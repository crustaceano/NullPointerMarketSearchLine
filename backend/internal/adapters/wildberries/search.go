package wildberries

import (
	"fmt"
	"strconv"
	"strings"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

type wbSearchResponse struct {
	Products []wbProduct `json:"products"`
	Data     struct {
		Products []wbProduct `json:"products"`
	} `json:"data"`
}

type wbProduct struct {
	ID            int64     `json:"id"`
	Brand         string    `json:"brand"`
	Name          string    `json:"name"`
	Supplier      string    `json:"supplier"`
	Rating        float64   `json:"rating"`
	ReviewRating  float64   `json:"reviewRating"`
	Feedbacks     int       `json:"feedbacks"`
	TotalQuantity int       `json:"totalQuantity"`
	Time1         int       `json:"time1"`
	Time2         int       `json:"time2"`
	Colors        []wbColor `json:"colors"`
	Sizes         []wbSize  `json:"sizes"`
}

type wbColor struct {
	Name string `json:"name"`
}

type wbSize struct {
	Name     string  `json:"name"`
	OrigName string  `json:"origName"`
	Price    wbPrice `json:"price"`
}

type wbPrice struct {
	Basic   int64 `json:"basic"`
	Product int64 `json:"product"`
	Total   int64 `json:"total"`
}

func parseWildberriesSearch(page []byte, region string) ([]models.ProductOffer, error) {
	var response wbSearchResponse
	if err := decodeWildberriesJSON(page, &response); err != nil {
		return nil, err
	}

	products := response.Products
	if len(products) == 0 {
		products = response.Data.Products
	}

	offers := make([]models.ProductOffer, 0, len(products))
	seen := map[int64]struct{}{}
	for _, product := range products {
		if len(offers) >= 12 {
			break
		}
		if _, ok := seen[product.ID]; ok {
			continue
		}
		offer, ok := wildberriesProductOffer(product, region)
		if !ok {
			continue
		}
		seen[product.ID] = struct{}{}
		offers = append(offers, offer)
	}

	return offers, nil
}

func wildberriesProductOffer(product wbProduct, region string) (models.ProductOffer, bool) {
	if product.ID <= 0 || product.TotalQuantity <= 0 {
		return models.ProductOffer{}, false
	}

	title := wildberriesTitle(product.Brand, product.Name)
	price := wildberriesPrice(product.Sizes)
	if title == "" || price <= 0 {
		return models.ProductOffer{}, false
	}

	return models.ProductOffer{
		Source:          "Wildberries",
		Title:           title,
		Image:           wildberriesImageURL(product.ID),
		Price:           price,
		Currency:        "RUB",
		URL:             wildberriesProductURL(product.ID),
		Characteristics: wildberriesSearchCharacteristics(product, region),
	}, true
}

func wildberriesTitle(brand, name string) string {
	brand = shared.CleanText(brand)
	name = shared.CleanText(name)
	if brand == "" {
		return name
	}
	if name == "" {
		return brand
	}
	if strings.Contains(strings.ToLower(name), strings.ToLower(brand)) {
		return name
	}
	return brand + " " + name
}

func wildberriesPrice(sizes []wbSize) float64 {
	var minPrice int64
	for _, size := range sizes {
		price := size.Price.Product
		if price == 0 {
			price = size.Price.Total
		}
		if price == 0 {
			price = size.Price.Basic
		}
		if price <= 0 {
			continue
		}
		if minPrice == 0 || price < minPrice {
			minPrice = price
		}
	}
	if minPrice <= 0 {
		return 0
	}
	return float64(minPrice) / 100
}

func wildberriesSearchCharacteristics(product wbProduct, region string) map[string]string {
	chars := map[string]string{
		"Регион":    region,
		"Источник":  "Wildberries",
		"В наличии": "да",
		"Остаток":   strconv.Itoa(product.TotalQuantity),
	}

	if brand := shared.CleanText(product.Brand); brand != "" {
		chars["Бренд"] = brand
	}
	if colors := wildberriesColors(product.Colors); colors != "" {
		chars["Цвет"] = colors
	}
	if sizes := wildberriesSizes(product.Sizes); sizes != "" {
		chars["Размеры"] = sizes
	}
	if product.ReviewRating > 0 {
		chars["Рейтинг"] = fmt.Sprintf("%.1f", product.ReviewRating)
	} else if product.Rating > 0 {
		chars["Рейтинг"] = fmt.Sprintf("%.1f", product.Rating)
	}
	if product.Feedbacks > 0 {
		chars["Отзывы"] = strconv.Itoa(product.Feedbacks)
	}
	if supplier := shared.CleanText(product.Supplier); supplier != "" {
		chars["Продавец"] = supplier
	}
	if delivery := wildberriesDelivery(product.Time1, product.Time2); delivery != "" {
		chars["Доставка"] = delivery
	}

	return chars
}

func wildberriesColors(colors []wbColor) string {
	values := make([]string, 0, len(colors))
	seen := map[string]struct{}{}
	for _, color := range colors {
		name := shared.CleanText(color.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, name)
		if len(values) >= 5 {
			break
		}
	}
	return strings.Join(values, ", ")
}

func wildberriesSizes(sizes []wbSize) string {
	values := make([]string, 0, len(sizes))
	seen := map[string]struct{}{}
	for _, size := range sizes {
		name := shared.CleanText(size.Name)
		if name == "" {
			name = shared.CleanText(size.OrigName)
		}
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, name)
		if len(values) >= 8 {
			break
		}
	}
	return strings.Join(values, ", ")
}

func wildberriesDelivery(time1, time2 int) string {
	switch {
	case time1 > 0 && time2 > 0:
		return fmt.Sprintf("%d-%d ч", time1, time2)
	case time2 > 0:
		return fmt.Sprintf("до %d ч", time2)
	case time1 > 0:
		return fmt.Sprintf("от %d ч", time1)
	default:
		return ""
	}
}
