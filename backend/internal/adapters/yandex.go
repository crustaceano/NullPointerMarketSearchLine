package adapters

import (
	"context"

	"nullpointer/backend/internal/models"
)

type YandexMarket struct{}

func NewYandexMarket() *YandexMarket { return &YandexMarket{} }

func (a *YandexMarket) Name() string { return "Yandex Market" }

func (a *YandexMarket) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 4990, "market.yandex.ru"), nil
}
