package wildberries

import (
	"context"
	"strings"
	"sync"
	"testing"

	"nullpointer/backend/internal/adapters/shared"
)

type fakeHTMLFetcher struct {
	mu        sync.Mutex
	body      []byte
	err       error
	url       string
	urls      []string
	headers   []map[string]string
	responses map[string][]byte
}

func (f *fakeHTMLFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.url == "" {
		f.url = rawURL
	}
	f.urls = append(f.urls, rawURL)
	f.headers = append(f.headers, shared.RequestHeaders(ctx))
	for urlPart, body := range f.responses {
		if strings.Contains(rawURL, urlPart) {
			return body, f.err
		}
	}
	return f.body, f.err
}

func TestWildberriesSearchParsesOffers(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		{
			"products": [
				{
					"id": 65417879,
					"brand": "ФЕСТА",
					"name": "Пальто для малышки",
					"supplier": "ФЕСТА",
					"reviewRating": 4.8,
					"feedbacks": 89,
					"totalQuantity": 55,
					"time1": 2,
					"time2": 24,
					"colors": [{"name": "бежевый"}, {"name": "белый"}],
					"sizes": [
						{"name": "80-86", "price": {"product": 428200}},
						{"name": "86-92", "price": {"product": 429900}}
					]
				}
			]
		}
	`)}

	offers, err := NewMarket(fetcher).Search(context.Background(), " пальто детское ", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	offer := offers[0]
	if offer.Source != "Wildberries" {
		t.Fatalf("Source = %q", offer.Source)
	}
	if offer.Title != "ФЕСТА Пальто для малышки" {
		t.Fatalf("Title = %q", offer.Title)
	}
	if offer.Price != 4282 {
		t.Fatalf("Price = %v", offer.Price)
	}
	if offer.URL != "https://www.wildberries.ru/catalog/65417879/detail.aspx" {
		t.Fatalf("URL = %q", offer.URL)
	}
	if !strings.Contains(offer.Image, "basket-04.wbbasket.ru/vol654/part65417/65417879") {
		t.Fatalf("Image = %q", offer.Image)
	}
	if offer.Characteristics["В наличии"] != "да" {
		t.Fatalf("availability = %q", offer.Characteristics["В наличии"])
	}
	if offer.Characteristics["Остаток"] != "55" {
		t.Fatalf("stock = %q", offer.Characteristics["Остаток"])
	}
	if offer.Characteristics["Рейтинг"] != "4.8" {
		t.Fatalf("rating = %q", offer.Characteristics["Рейтинг"])
	}
	if offer.Characteristics["Отзывы"] != "89" {
		t.Fatalf("feedbacks = %q", offer.Characteristics["Отзывы"])
	}
	if offer.Characteristics["Размеры"] != "80-86, 86-92" {
		t.Fatalf("sizes = %q", offer.Characteristics["Размеры"])
	}
	if !strings.Contains(fetcher.url, "query=%D0%BF%D0%B0%D0%BB%D1%8C%D1%82%D0%BE+%D0%B4%D0%B5%D1%82%D1%81%D0%BA%D0%BE%D0%B5") {
		t.Fatalf("fetch url does not contain escaped query: %s", fetcher.url)
	}
	if !strings.Contains(fetcher.url, "dest=1259570991") {
		t.Fatalf("fetch url does not contain Moscow dest: %s", fetcher.url)
	}
	if !strings.HasPrefix(fetcher.url, "https://u-search.wb.ru/exactmatch/ru/common/v18/search?") {
		t.Fatalf("fetch url host = %s", fetcher.url)
	}
	if fetcher.headers[0]["Accept"] != "application/json, text/plain, */*" {
		t.Fatalf("Accept header = %q", fetcher.headers[0]["Accept"])
	}
	if !strings.Contains(fetcher.headers[0]["Referer"], "wildberries.ru/catalog/0/search.aspx") {
		t.Fatalf("Referer header = %q", fetcher.headers[0]["Referer"])
	}
}

func TestWildberriesSearchEnrichesProductDetails(t *testing.T) {
	searchPage := []byte(`
		{
			"products": [
				{
					"id": 65417879,
					"brand": "ФЕСТА",
					"name": "Пальто",
					"totalQuantity": 3,
					"sizes": [{"name": "80-86", "price": {"product": 428200}}]
				}
			]
		}
	`)
	detailPage := []byte(`
		{
			"subj_name": "Пальто",
			"season": "Демисезон",
			"contents": "пальто",
			"options": [
				{"name": "Состав", "value": "искусственная шерсть"},
				{"name": "Пол", "value": "Девочки"},
				{"name": "Артикул", "value": "hidden"}
			]
		}
	`)
	fetcher := &fakeHTMLFetcher{
		body: searchPage,
		responses: map[string][]byte{
			"/info/ru/card.json": detailPage,
		},
	}

	offers, err := NewMarket(fetcher).Search(context.Background(), "пальто", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	chars := offers[0].Characteristics
	if chars["Категория"] != "Пальто" {
		t.Fatalf("category = %q", chars["Категория"])
	}
	if chars["Сезон"] != "Демисезон" {
		t.Fatalf("season = %q", chars["Сезон"])
	}
	if chars["Состав"] != "искусственная шерсть" {
		t.Fatalf("composition = %q", chars["Состав"])
	}
	if chars["Пол"] != "Девочки" {
		t.Fatalf("gender = %q", chars["Пол"])
	}
	if _, ok := chars["Артикул"]; ok {
		t.Fatal("article should not be exposed as a user characteristic")
	}
	if len(fetcher.urls) != 2 {
		t.Fatalf("fetch count = %d, want 2", len(fetcher.urls))
	}
	if !strings.Contains(fetcher.urls[1], "basket-04.wbbasket.ru/vol654/part65417/65417879/info/ru/card.json") {
		t.Fatalf("detail url = %q", fetcher.urls[1])
	}
	if fetcher.headers[1]["Accept"] != "application/json, text/plain, */*" {
		t.Fatalf("detail Accept header = %q", fetcher.headers[1]["Accept"])
	}
}

func TestParseWildberriesSearchSupportsNestedProductsAndSkipsOutOfStock(t *testing.T) {
	page := []byte(`
		{
			"data": {
				"products": [
					{"id": 1, "brand": "NoStock", "name": "Пальто", "totalQuantity": 0, "sizes": [{"price": {"product": 10000}}]},
					{"id": 14400000, "brand": "Stock", "name": "Пальто", "totalQuantity": 1, "sizes": [{"price": {"product": 123400}}]}
				]
			}
		}
	`)

	offers, err := parseWildberriesSearch(page, "Москва")
	if err != nil {
		t.Fatalf("parseWildberriesSearch() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
	if offers[0].Title != "Stock Пальто" {
		t.Fatalf("Title = %q", offers[0].Title)
	}
}

func TestWildberriesURLs(t *testing.T) {
	if got := wildberriesImageURL(65417879); !strings.Contains(got, "basket-04.wbbasket.ru/vol654/part65417/65417879/images/c516x688/1.webp") {
		t.Fatalf("image url = %q", got)
	}
	if got := wildberriesIDFromProductURL("https://www.wildberries.ru/catalog/65417879/detail.aspx"); got != 65417879 {
		t.Fatalf("id = %d", got)
	}
}

func TestWildberriesBasketSupportsNewVolumeRanges(t *testing.T) {
	cases := map[int64]string{
		4961: "27",
		5279: "28",
		5776: "29",
		5833: "30",
		6191: "31",
		6499: "32",
		7548: "35",
		7975: "36",
		8211: "37",
		8938: "39",
		9527: "40",
	}

	for vol, want := range cases {
		if got := wildberriesBasket(vol); got != want {
			t.Fatalf("wildberriesBasket(%d) = %q, want %q", vol, got, want)
		}
	}
}

func TestWildberriesSearchURLUsesRegionDest(t *testing.T) {
	url := wildberriesSearchURL("ноутбук", "Санкт-Петербург")

	if !strings.Contains(url, "dest=-1198055") {
		t.Fatalf("url does not contain Saint Petersburg dest: %s", url)
	}
}

func TestWildberriesSearchURLUsesExtendedRegionDest(t *testing.T) {
	url := wildberriesSearchURL("ноутбук", "Могилёв")

	if !strings.Contains(url, "dest=-82606") {
		t.Fatalf("url does not contain Mogilev dest: %s", url)
	}
}
