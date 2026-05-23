package adapters

import (
	"context"
	"strings"
	"testing"
)

type fakeHTMLFetcher struct {
	body []byte
	err  error
	url  string
}

func (f *fakeHTMLFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.url = rawURL
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

	offers, err := NewYandexMarket(fetcher).Search(context.Background(), " ноутбук ", "Москва")
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
