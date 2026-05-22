package adapters

import (
	"context"

	"nullpointer/backend/internal/models"
)

type Wildberries struct{}

func NewWildberries() *Wildberries { return &Wildberries{} }

func (a *Wildberries) Name() string { return "Wildberries" }

func (a *Wildberries) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 4690, "wildberries.ru"), nil
}
