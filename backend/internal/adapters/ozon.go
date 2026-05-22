package adapters

import (
	"context"

	"nullpointer/backend/internal/models"
)

type Ozon struct{}

func NewOzon() *Ozon { return &Ozon{} }

func (a *Ozon) Name() string { return "Ozon" }

func (a *Ozon) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	// TODO: replace mock with a real HTTP+HTML parser.
	return mockOffers(a.Name(), query, region, 5290, "ozon.ru"), nil
}
