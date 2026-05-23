package ozon

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

func TestOzonSearchAddsBaseCharacteristics(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html><body>
			<div>
				<img src="//cdn.ozon.ru/phone.webp">
				<a href="/product/phone-123/">Смартфон Test 128 ГБ</a>
				<span>41 990 ₽</span>
			</div>
		</body></html>
	`)}

	offers, err := NewOzon(fetcher).Search(context.Background(), " смартфон ", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	chars := offers[0].Characteristics
	if chars["Регион"] != "Москва" {
		t.Fatalf("region = %q", chars["Регион"])
	}
	if chars["Источник"] != "Ozon" {
		t.Fatalf("source = %q", chars["Источник"])
	}
	if chars["В наличии"] != "да" {
		t.Fatalf("availability = %q", chars["В наличии"])
	}
}

func TestOzonSearchExtractsCharacteristicsFromTitle(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html><body>
			<div>
				<img src="//cdn.ozon.ru/phone.webp">
				<a href="/product/phone-123/">tecno Смартфон Spark Go 2 Ростест (EAC) 4/128 ГБ, Nano-SIM, серый</a>
				<span>6 990 ₽</span>
			</div>
		</body></html>
	`)}

	offers, err := NewOzon(fetcher).Search(context.Background(), "телефон", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}

	chars := offers[0].Characteristics
	if chars["Бренд"] != "tecno" {
		t.Fatalf("brand = %q", chars["Бренд"])
	}
	if chars["Оперативная память"] != "4 ГБ" {
		t.Fatalf("ram = %q", chars["Оперативная память"])
	}
	if chars["Встроенная память"] != "128 ГБ" {
		t.Fatalf("storage = %q", chars["Встроенная память"])
	}
	if chars["SIM-карта"] != "Nano-SIM" {
		t.Fatalf("sim = %q", chars["SIM-карта"])
	}
	if chars["Цвет"] != "серый" {
		t.Fatalf("color = %q", chars["Цвет"])
	}
	if chars["Сертификация"] != "Ростест (EAC)" {
		t.Fatalf("certification = %q", chars["Сертификация"])
	}
}

func TestParseOzonProductDetailsParsesDefinitionListAndTable(t *testing.T) {
	page := []byte(`
		<html><body>
			<dl>
				<dt>Бренд</dt><dd>Lenovo</dd>
				<dt>Оперативная память</dt><dd>16 ГБ</dd>
				<dt>Артикул</dt><dd>hidden</dd>
			</dl>
			<table>
				<tr><th>Процессор</th><td>Intel Core i7</td></tr>
			</table>
		</body></html>
	`)

	chars, err := parseOzonProductDetails(page)
	if err != nil {
		t.Fatalf("parseOzonProductDetails() error = %v", err)
	}
	if chars["Бренд"] != "Lenovo" {
		t.Fatalf("brand = %q", chars["Бренд"])
	}
	if chars["Оперативная память"] != "16 ГБ" {
		t.Fatalf("ram = %q", chars["Оперативная память"])
	}
	if chars["Процессор"] != "Intel Core i7" {
		t.Fatalf("cpu = %q", chars["Процессор"])
	}
	if _, ok := chars["Артикул"]; ok {
		t.Fatal("article should not be exposed as a user characteristic")
	}
}

func TestParseOzonProductDetailsParsesWidgetText(t *testing.T) {
	page := []byte(`
		<div data-widget="webCharacteristics">
			<div>Материал корпуса: алюминий</div>
			<div>Цвет — серый</div>
		</div>
	`)

	chars, err := parseOzonProductDetails(page)
	if err != nil {
		t.Fatalf("parseOzonProductDetails() error = %v", err)
	}
	if chars["Материал корпуса"] != "алюминий" {
		t.Fatalf("material = %q", chars["Материал корпуса"])
	}
	if chars["Цвет"] != "серый" {
		t.Fatalf("color = %q", chars["Цвет"])
	}
}

func TestParseOzonProductDetailsParsesDataState(t *testing.T) {
	page := []byte(`
		<div data-widget="webCharacteristics" data-state='{
			"groups": [{
				"characteristics": [
					{"title": "Бренд", "values": [{"text": "realme"}]},
					{"title": "Оперативная память", "values": [{"text": "3 ГБ"}]},
					{"title": "Встроенная память", "values": [{"text": "64 ГБ"}]},
					{"title": "Цвет", "value": "черный"},
					{"title": "Артикул", "value": "hidden"}
				]
			}]
		}'></div>
	`)

	chars, err := parseOzonProductDetails(page)
	if err != nil {
		t.Fatalf("parseOzonProductDetails() error = %v", err)
	}
	if chars["Бренд"] != "realme" {
		t.Fatalf("brand = %q", chars["Бренд"])
	}
	if chars["Оперативная память"] != "3 ГБ" {
		t.Fatalf("ram = %q", chars["Оперативная память"])
	}
	if chars["Встроенная память"] != "64 ГБ" {
		t.Fatalf("storage = %q", chars["Встроенная память"])
	}
	if chars["Цвет"] != "черный" {
		t.Fatalf("color = %q", chars["Цвет"])
	}
	if _, ok := chars["Артикул"]; ok {
		t.Fatal("article should not be exposed as a user characteristic")
	}
}

func TestBuildOzonSearchURLDoesNotSendRegionParam(t *testing.T) {
	url := buildOzonSearchURL(" ноутбук ", "питер")

	if !strings.HasPrefix(url, "https://www.ozon.ru/search/?") {
		t.Fatalf("url = %s", url)
	}
	if !strings.Contains(url, "text=%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA") {
		t.Fatalf("url does not contain escaped query: %s", url)
	}
	if strings.Contains(url, "region=") {
		t.Fatalf("url should not contain unsupported region parameter: %s", url)
	}
	if !strings.Contains(url, "from_global=true") {
		t.Fatalf("url does not contain from_global=true: %s", url)
	}
}

func TestOzonSearchFiltersSaleFallbackCards(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html><body>
			<div>
				<img src="//cdn.ozon.ru/socks.webp">
				<a href="/product/socks-111/">Распродажа</a>
				<span>311 ₽</span>
			</div>
			<div>
				<img src="//cdn.ozon.ru/phone.webp">
				<a href="/product/phone-222/">Смартфон realme Note 60x Ростест (EAC) 3/64 ГБ, черный</a>
				<span>5 695 ₽</span>
			</div>
		</body></html>
	`)}

	offers, err := NewOzon(fetcher).Search(context.Background(), "телефон", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
	if offers[0].Title != "Смартфон realme Note 60x Ростест (EAC) 3/64 ГБ, черный" {
		t.Fatalf("title = %q", offers[0].Title)
	}
}

func TestOzonSearchDropsOverlongContainerTitle(t *testing.T) {
	longTitle := strings.Repeat("Возможно, вам понравится Распродажа 311 ₽ Носки женские ", 5)
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html><body>
			<div>
				<img src="//cdn.ozon.ru/junk.webp">
				<a href="/product/junk-111/">` + longTitle + `</a>
				<span>311 ₽</span>
			</div>
			<div>
				<img src="//cdn.ozon.ru/phone.webp">
				<a href="/product/phone-222/">Смартфон Samsung Galaxy A07 4/64 ГБ, черный</a>
				<span>7 226 ₽</span>
			</div>
		</body></html>
	`)}

	offers, err := NewOzon(fetcher).Search(context.Background(), "телефон", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("len(offers) = %d, want 1", len(offers))
	}
	if offers[0].Title != "Смартфон Samsung Galaxy A07 4/64 ГБ, черный" {
		t.Fatalf("title = %q", offers[0].Title)
	}
}

func TestOzonSearchDoesNotReturnIrrelevantFallbackCards(t *testing.T) {
	fetcher := &fakeHTMLFetcher{body: []byte(`
		<html><body>
			<div>
				<img src="//cdn.ozon.ru/cap.webp">
				<a href="/product/cap-111/">Бейсболка</a>
				<span>322 ₽</span>
			</div>
			<div>
				<img src="//cdn.ozon.ru/shirt.webp">
				<a href="/product/shirt-222/">Рубашка</a>
				<span>322 ₽</span>
			</div>
			<div>
				<img src="//cdn.ozon.ru/cable.webp">
				<a href="/product/cable-333/">Клемма на 2 провода Wago 221-412</a>
				<span>322 ₽</span>
			</div>
		</body></html>
	`)}

	offers, err := NewOzon(fetcher).Search(context.Background(), "пальто детское", "Москва")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(offers) != 0 {
		t.Fatalf("len(offers) = %d, want 0: %#v", len(offers), offers)
	}
}
