package adapters

import (
	"context"
	"time"

	"nullpointer/backend/internal/adapters/ozon"
	"nullpointer/backend/internal/adapters/wildberries"
	"nullpointer/backend/internal/adapters/yandex"
	"nullpointer/backend/internal/models"
)

// SourceAdapter is the contract every product source (Yandex, Ozon, ...) implements.
type SourceAdapter interface {
	Name() string
	Search(ctx context.Context, query string, region string) ([]models.ProductOffer, error)
}

// All returns the default set of adapters used by the service.
func All() []SourceAdapter {
	// Общий fetcher для старых адаптеров, которые пока лежат в package adapters.
	baseFetcher := NewHTMLFetcher(FetcherConfig{
		Timeout: 6 * time.Second,
	})

	smartFetcher := NewSmartAntiCaptchaFetcher(baseFetcher)

	// Отдельный fetcher только для Ozon.
	ozonBaseFetcher := ozon.NewHTMLFetcher(ozon.FetcherConfig{
		Timeout: 30 * time.Second,
	})

	ozonSmartFetcher := ozon.NewSmartAntiCaptchaFetcher(ozonBaseFetcher)

	return []SourceAdapter{
		yandex.NewMarket(smartFetcher),
		ozon.NewOzon(ozonSmartFetcher),
		wildberries.NewMarket(smartFetcher),
		NewRunetSource(smartFetcher),
	}
}
