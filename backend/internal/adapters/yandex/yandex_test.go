package yandex

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type fakeHTMLFetcher struct {
	mu        sync.Mutex
	body      []byte
	err       error
	url       string
	urls      []string
	responses map[string][]byte
}

func (f *fakeHTMLFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.url == "" {
		f.url = rawURL
	}
	f.urls = append(f.urls, rawURL)
	for urlPart, body := range f.responses {
		if strings.Contains(rawURL, urlPart) {
			return body, f.err
		}
	}
	return f.body, f.err
}

func TestYandexMarketSearchParsesOffers(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html>
			<body>
				<article data-auto="searchOrganic">
					<a data-auto="snippet-link" href="/product--noutbuk-test-15/123">Ноутбук Test 15</a>
					<img alt="Ноутбук Test 15" src="//avatars.mds.yandex.net/get-mpic/123/img_id/orig" />
					<span>52 990 ₽</span>
					<span>Рейтинг 4,8</span>
					<span>145 отзывов</span>
					<span>Доставка завтра</span>
					<span>В наличии</span>
				</article>
				<article data-auto="searchOrganic">
					<a data-auto="snippet-link" href="https://market.yandex.ru/card/noutbuk-second/456">Ноутбук Second</a>
					<img alt="Ноутбук Second" data-src="https://avatars.mds.yandex.net/get-mpic/456/img_id/orig" />
					<span>41&nbsp;500 ₽</span>
				</article>
			</body>
		</html>
	`)}

	offers, err := NewMarket(fetcher).Search(context.Background(), " ноутбук ", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 2 {
		t.Fatalf("len(offers) = %d, want 2", len(offers))
	}

	first := offers[0]
	if first.Source != "Yandex Market" {
		t.Fatalf("Source = %q", first.Source)
	}
	if first.Title != "Ноутбук Test 15" {
		t.Fatalf("Title = %q", first.Title)
	}
	if first.Price != 52990 {
		t.Fatalf("Price = %v", first.Price)
	}
	if first.URL != "https://market.yandex.ru/product--noutbuk-test-15/123" {
		t.Fatalf("URL = %q", first.URL)
	}
	if !strings.HasPrefix(first.Image, "https://avatars.mds.yandex.net/") {
		t.Fatalf("Image = %q", first.Image)
	}
	if first.Characteristics["Регион"] != "Москва" {
		t.Fatalf("region characteristic = %q", first.Characteristics["Регион"])
	}
	if first.Characteristics["В наличии"] != "да" {
		t.Fatalf("availability characteristic = %q", first.Characteristics["В наличии"])
	}
	if first.Characteristics["Рейтинг"] != "4.8" {
		t.Fatalf("rating characteristic = %q", first.Characteristics["Рейтинг"])
	}
	if first.Characteristics["Отзывы"] != "145" {
		t.Fatalf("reviews characteristic = %q", first.Characteristics["Отзывы"])
	}
	if first.Characteristics["Доставка"] == "" {
		t.Fatal("delivery characteristic is empty")
	}

	if !strings.Contains(fetcher.url, "text=%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA") {
		t.Fatalf("fetch url does not contain escaped query: %s", fetcher.url)
	}
	if !strings.Contains(fetcher.url, "lr=213") {
		t.Fatalf("fetch url does not contain Moscow lr: %s", fetcher.url)
	}
}

func TestYandexSearchURLUsesRegionLR(t *testing.T) {
	url := yandexSearchURL("ноутбук", "Санкт-Петербург")

	if !strings.Contains(url, "lr=2") {
		t.Fatalf("url does not contain Saint Petersburg lr: %s", url)
	}
}

func TestParseYandexMarketOffersDeduplicatesLinks(t *testing.T) {
	page := []byte(`
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/same/1">Ноутбук Same</a>
			<img alt="Ноутбук Same" src="//avatars.mds.yandex.net/get-mpic/1/img" />
			<span>10 000 ₽</span>
		</article>
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/same/1">Ноутбук Same duplicate</a>
			<img alt="Ноутбук Same duplicate" src="//avatars.mds.yandex.net/get-mpic/1/img" />
			<span>11 000 ₽</span>
		</article>
	`)

	offers, err := parseYandexMarketOffers(page, "Москва")
	if err != nil {
		t.Fatalf("parseYandexMarketOffers() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
}

func TestParseYandexMarketOffersSkipsUnavailableBySelectors(t *testing.T) {
	page := []byte(`
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/paper-missing/1">Бумага А4 500 листов</a>
			<img alt="Бумага А4 500 листов" src="//avatars.mds.yandex.net/get-mpic/1/img" />
			<div data-auto="snippet-price-current">399 ₽</div>
			<div data-auto="availability">Нет в продаже</div>
			<button>Сообщить о поступлении</button>
		</article>
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/paper-ok/2">Бумага офисная А4</a>
			<img alt="Бумага офисная А4" src="//avatars.mds.yandex.net/get-mpic/2/img" />
			<div data-auto="snippet-price-current">499 ₽</div>
			<div data-auto="availability">В наличии</div>
			<div data-auto="rating">Рейтинг 4,7</div>
			<a href="/card/paper-ok/2/reviews">82 отзыва</a>
		</article>
	`)

	offers, err := parseYandexMarketOffers(page, "Москва")
	if err != nil {
		t.Fatalf("parseYandexMarketOffers() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
	if offers[0].Title != "Бумага офисная А4" {
		t.Fatalf("offers[0].Title = %q", offers[0].Title)
	}
	if offers[0].Characteristics["В наличии"] != "да" {
		t.Fatalf("availability = %q", offers[0].Characteristics["В наличии"])
	}
	if offers[0].Characteristics["Рейтинг"] != "4.7" {
		t.Fatalf("rating = %q", offers[0].Characteristics["Рейтинг"])
	}
	if offers[0].Characteristics["Отзывы"] != "82" {
		t.Fatalf("reviews = %q", offers[0].Characteristics["Отзывы"])
	}
}

func TestParseYandexMarketOffersUsesApiaryFallback(t *testing.T) {
	page := []byte(`
		<html>
			<body>
				<noframes data-apiary="patch">{"widgets":{"@marketfront/SnippetConstructor/SimpleGallery/ImageManager":{"/content/serpEntity100/gallery/imageManager":{"propsToCreateImages":{"pictures":[{"picture":{"baseUrl":"https://avatars.mds.yandex.net/get-mpic/99/apiary-image/"}}]}}}}}</noframes>
				<noframes data-apiary="patch">{"widgets":{"@light/AddToCartButton":{"/content/serpEntity100/addToCartButton":{"pendingCartItem":{"productId":111,"skuId":222,"price":{"value":1299900000000,"currency":"RUR","valueFmt":"129990"},"name":"Ноутбук Apiary 16","count":7,"imageMeta":{"key":"apiary-image","namespace":"mpic","groupId":99}},"quantityMaximum":7,"disableAddItem":false,"isError":false}}}}</noframes>
			</body>
		</html>
	`)

	offers, err := parseYandexMarketOffers(page, "Москва")
	if err != nil {
		t.Fatalf("parseYandexMarketOffers() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	offer := offers[0]
	if offer.Title != "Ноутбук Apiary 16" {
		t.Fatalf("Title = %q", offer.Title)
	}
	if offer.Price != 129990 {
		t.Fatalf("Price = %v", offer.Price)
	}
	if offer.Currency != "RUB" {
		t.Fatalf("Currency = %q", offer.Currency)
	}
	if !strings.HasSuffix(offer.Image, "/orig") {
		t.Fatalf("Image = %q", offer.Image)
	}
	if !strings.Contains(offer.URL, "market.yandex.ru/search") {
		t.Fatalf("URL = %q", offer.URL)
	}
	if offer.Characteristics["В наличии"] != "да" {
		t.Fatalf("availability = %q", offer.Characteristics["В наличии"])
	}
	if offer.Characteristics["Остаток"] != "7" {
		t.Fatalf("stock = %q", offer.Characteristics["Остаток"])
	}
}

func TestParseYandexMarketOffersEnrichesDOMFromApiary(t *testing.T) {
	page := []byte(`
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/noutbuk-apiary-dom/222">Ноутбук Apiary DOM</a>
		</article>
		<noframes data-apiary="patch">{"widgets":{"@light/AddToCartButton":{"/content/serpEntity100/addToCartButton":{"pendingCartItem":{"productId":111,"skuId":222,"price":{"value":0,"currency":"RUR","valueFmt":"88990"},"name":"Ноутбук Apiary DOM","count":3,"imageMeta":{"key":"dom-image","namespace":"mpic","groupId":100}},"quantityMaximum":3,"disableAddItem":false,"isError":false}}}}</noframes>
	`)

	offers, err := parseYandexMarketOffers(page, "Москва")
	if err != nil {
		t.Fatalf("parseYandexMarketOffers() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
	if offers[0].URL != "https://market.yandex.ru/card/noutbuk-apiary-dom/222" {
		t.Fatalf("URL = %q", offers[0].URL)
	}
	if offers[0].Price != 88990 {
		t.Fatalf("Price = %v", offers[0].Price)
	}
	if !strings.Contains(offers[0].Image, "dom-image") {
		t.Fatalf("Image = %q", offers[0].Image)
	}
	if offers[0].Characteristics["Остаток"] != "3" {
		t.Fatalf("stock = %q", offers[0].Characteristics["Остаток"])
	}
}

func TestYandexMarketSearchEnrichesOffersWithProductDetails(t *testing.T) {
	searchPage := []byte(`
		<article data-auto="searchOrganic">
			<a data-auto="snippet-link" href="/card/noutbuk-detail/123">Ноутбук Detail</a>
			<img alt="Ноутбук Detail" src="//avatars.mds.yandex.net/get-mpic/1/img" />
			<div data-auto="snippet-price-current">75 000 ₽</div>
			<div data-auto="availability">В наличии</div>
		</article>
	`)
	productPage := []byte(`
		<div data-auto="product-full-specs">
			<div data-auto="specs-list-fullExtended">
				<div>
					<span data-auto="product-spec">Бренд</span>
					<span>Lenovo</span>
				</div>
				<div>
					<span data-auto="product-spec">Процессор</span>
					<span>Intel Core i7</span>
				</div>
				<div>
					<span data-auto="product-spec">Оперативная память</span>
					<span>16 ГБ</span>
				</div>
				<div>
					<span data-auto="product-spec">Артикул Маркета</span>
					<span>123</span>
				</div>
			</div>
		</div>
	`)
	fetcher := &fakeHTMLFetcher{
		body: searchPage,
		responses: map[string][]byte{
			"/card/noutbuk-detail/123": productPage,
		},
	}

	offers, err := NewMarket(fetcher).Search(context.Background(), "ноутбук", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	chars := offers[0].Characteristics
	if chars["Бренд"] != "Lenovo" {
		t.Fatalf("brand = %q", chars["Бренд"])
	}
	if chars["Процессор"] != "Intel Core i7" {
		t.Fatalf("cpu = %q", chars["Процессор"])
	}
	if chars["Оперативная память"] != "16 ГБ" {
		t.Fatalf("ram = %q", chars["Оперативная память"])
	}
	if _, ok := chars["Артикул Маркета"]; ok {
		t.Fatal("market article should not be exposed as a user characteristic")
	}
	if len(fetcher.urls) != 2 {
		t.Fatalf("fetch count = %d, want 2", len(fetcher.urls))
	}
}

func TestParseYandexProductDetailsParsesMinimalSpecs(t *testing.T) {
	page := []byte(`
		<div data-auto="specs-list-minimal">
			<div>
				<span data-auto="product-spec">Диагональ экрана</span>
				<div><span>14.1"</span></div>
			</div>
			<div>
				<span data-auto="product-spec">Разрешение экрана</span>
				<div><span>1920x1080</span></div>
			</div>
		</div>
	`)

	chars, err := parseYandexProductDetails(page)
	if err != nil {
		t.Fatalf("parseYandexProductDetails() error = %v", err)
	}
	if chars["Диагональ экрана"] != `14.1"` {
		t.Fatalf("screen = %q", chars["Диагональ экрана"])
	}
	if chars["Разрешение экрана"] != "1920x1080" {
		t.Fatalf("resolution = %q", chars["Разрешение экрана"])
	}
}
