package adapters

import (
	"context"
	"nullpointer/backend/internal/adapters/ozon"

	"nullpointer/backend/internal/models"
)

type YandexMarket struct {
	fetcher ozon.HTMLFetcher
}

func NewYandexMarket(fetcher ozon.HTMLFetcher) *YandexMarket {
	return &YandexMarket{fetcher: fetcher}
}

func (a *YandexMarket) Name() string { return "Yandex Market" }

func (a *YandexMarket) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 4990, "market.yandex.ru"), nil
}
