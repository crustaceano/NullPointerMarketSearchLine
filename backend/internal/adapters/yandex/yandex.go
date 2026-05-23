package yandex

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
)

const yandexMarketHost = "https://market.yandex.ru"

const yandexResultLimit = 8

type YandexMarket struct {
	fetcher shared.HTMLFetcher
}

func NewMarket(fetcher shared.HTMLFetcher) *YandexMarket {
	return &YandexMarket{fetcher: fetcher}
}

func (a *YandexMarket) Name() string { return "Yandex Market" }

func (a *YandexMarket) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	page, err := a.fetcher.Fetch(ctx, yandexSearchURL(query))
	if err != nil {
		return nil, fmt.Errorf("yandex market fetch: %w", err)
	}

	offers, err := parseYandexMarketOffers(page, region)
	if err != nil {
		return nil, err
	}
	if len(offers) == 0 {
		return nil, errors.New("yandex market returned no parsable offers")
	}

	offers = shared.LimitOffers(offers, yandexResultLimit)
	a.enrichOffersWithProductDetails(ctx, offers)
	return offers, nil
}

func yandexSearchURL(query string) string {
	values := url.Values{}
	values.Set("text", query)
	values.Set("cvredirect", "0")
	return yandexMarketHost + "/search?" + values.Encode()
}
