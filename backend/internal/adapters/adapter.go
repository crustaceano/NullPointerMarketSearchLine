package adapters

import (
	"context"

	"nullpointer/backend/internal/models"
)

// SourceAdapter is the contract every product source (Yandex, Ozon, ...) implements.
// Real HTTP/HTML parsers can later replace the mock implementations without
// changing anything else in the pipeline.
type SourceAdapter interface {
	Name() string
	Search(ctx context.Context, query string, region string) ([]models.ProductOffer, error)
}

// All returns the default set of adapters used by the service.
func All() []SourceAdapter {
	return []SourceAdapter{
		NewYandexMarket(),
		NewOzon(),
		NewWildberries(),
		NewRunetSource(),
	}
}
