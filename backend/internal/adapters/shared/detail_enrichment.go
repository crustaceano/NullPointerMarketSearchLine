package shared

import (
	"context"
	"sync"
	"time"

	"nullpointer/backend/internal/models"
)

type HTMLFetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

type DetailEnrichmentConfig struct {
	Concurrency int
	Timeout     time.Duration
	ShouldFetch func(rawURL string) bool
	BuildURL    func(offer models.ProductOffer) string
	Parse       func(page []byte) (map[string]string, error)
	OnResult    func(DetailEnrichmentResult)
}

type DetailEnrichmentResult struct {
	URL            string
	Page           []byte
	PageBytes      int
	Characteristic int
	FetchErr       error
	ParseErr       error
}

func EnrichOfferDetails(ctx context.Context, fetcher HTMLFetcher, offers []models.ProductOffer, cfg DetailEnrichmentConfig) {
	if len(offers) == 0 || fetcher == nil || cfg.ShouldFetch == nil || cfg.Parse == nil {
		return
	}

	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i := range offers {
		if !cfg.ShouldFetch(offers[i].URL) {
			continue
		}

		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			detailCtx := ctx
			cancel := func() {}
			if cfg.Timeout > 0 {
				detailCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
			}
			defer cancel()

			detailURL := offers[index].URL
			if cfg.BuildURL != nil {
				detailURL = cfg.BuildURL(offers[index])
			}
			if detailURL == "" {
				return
			}

			page, err := fetcher.Fetch(detailCtx, detailURL)
			if err != nil {
				reportDetailEnrichmentResult(cfg, DetailEnrichmentResult{URL: detailURL, FetchErr: err})
				return
			}
			chars, err := cfg.Parse(page)
			if err != nil || len(chars) == 0 {
				reportDetailEnrichmentResult(cfg, DetailEnrichmentResult{
					URL:            detailURL,
					Page:           page,
					PageBytes:      len(page),
					Characteristic: len(chars),
					ParseErr:       err,
				})
				return
			}
			reportDetailEnrichmentResult(cfg, DetailEnrichmentResult{
				URL:            detailURL,
				Page:           page,
				PageBytes:      len(page),
				Characteristic: len(chars),
			})
			offers[index].Characteristics = MergeCharacteristics(offers[index].Characteristics, chars)
		}(i)
	}

	wg.Wait()
}

func reportDetailEnrichmentResult(cfg DetailEnrichmentConfig, result DetailEnrichmentResult) {
	if cfg.OnResult != nil {
		cfg.OnResult(result)
	}
}
