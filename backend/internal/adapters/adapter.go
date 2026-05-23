package adapters

import (
	"context"
	"time"

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
	// 1. Создаем ваш базовый быстрый фетчер
	baseFetcher := NewHTMLFetcher(FetcherConfig{
		Timeout: 6 * time.Second,
	})

	// 2. Оборачиваем его в систему автоматического обхода капчи через Chromium
	smartFetcher := NewSmartAntiCaptchaFetcher(baseFetcher)

	// 3. Передаем smartFetcher во ВСЕ маркетплейсы
	return []SourceAdapter{
		yandex.NewMarket(smartFetcher),
		NewOzon(smartFetcher),
		NewWildberries(smartFetcher),
		NewRunetSource(smartFetcher),
	}
}
