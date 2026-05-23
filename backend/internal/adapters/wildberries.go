package adapters

import (
	"context"
	"nullpointer/backend/internal/adapters/ozon"

	"nullpointer/backend/internal/models"
)

type Wildberries struct {
	fetcher ozon.HTMLFetcher
}

func NewWildberries(fetcher ozon.HTMLFetcher) *Wildberries {
	return &Wildberries{fetcher: fetcher}
}

func (a *Wildberries) Name() string { return "Wildberries" }

func (a *Wildberries) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 4690, "wildberries.ru"), nil
}
