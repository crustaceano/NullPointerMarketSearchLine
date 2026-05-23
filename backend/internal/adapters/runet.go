package adapters

import (
	"context"

	"nullpointer/backend/internal/models"
)

// RunetSource is a non-marketplace Runet shop (e.g. citilink.ru / dns-shop.ru / sportmaster.ru).
// We keep its identity configurable to satisfy the "non-fixed" requirement.
type RunetSource struct {
	name    string
	host    string
	fetcher HTMLFetcher
}

func NewRunetSource(fetcher HTMLFetcher) *RunetSource {
	return &RunetSource{name: "Citilink", host: "citilink.ru", fetcher: fetcher}
}

func (a *RunetSource) Name() string { return a.name }

func (a *RunetSource) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 5590, a.host), nil
}
