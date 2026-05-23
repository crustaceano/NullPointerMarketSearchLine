package wildberries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"nullpointer/backend/internal/adapters/shared"
	"nullpointer/backend/internal/models"
	"nullpointer/backend/internal/regions"
)

const (
	wildberriesHost        = "https://www.wildberries.ru"
	wildberriesSearchHost  = "https://u-search.wb.ru"
	wildberriesResultLimit = 8
)

type Wildberries struct {
	fetcher shared.HTMLFetcher
}

func NewMarket(fetcher shared.HTMLFetcher) *Wildberries {
	return &Wildberries{fetcher: fetcher}
}

func (a *Wildberries) Name() string { return "Wildberries" }

func (a *Wildberries) Search(ctx context.Context, query, region string) ([]models.ProductOffer, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := wildberriesSearchURL(query, region)
	page, err := a.fetcher.Fetch(wildberriesRequestContext(ctx, wildberriesSearchPageURL(query)), searchURL)
	if err != nil {
		return nil, fmt.Errorf("wildberries fetch: %w", err)
	}

	offers, err := parseWildberriesSearch(page, region)
	if err != nil {
		return nil, err
	}
	if len(offers) == 0 {
		return nil, errors.New("wildberries returned no parsable offers")
	}

	offers = shared.LimitOffers(offers, wildberriesResultLimit)
	a.enrichOffersWithProductDetails(ctx, offers)
	return offers, nil
}

func wildberriesSearchURL(query, region string) string {
	values := url.Values{}
	values.Set("ab_testid", "new_benefit_sort")
	values.Set("appType", "1")
	values.Set("curr", "rub")
	values.Set("dest", wildberriesDest(region))
	values.Set("inheritFilters", "false")
	values.Set("lang", "ru")
	values.Set("page", "1")
	values.Set("query", query)
	values.Set("resultset", "catalog")
	values.Set("sort", "popular")
	values.Set("spp", "30")
	values.Set("suppressSpellcheck", "false")
	return wildberriesSearchHost + "/exactmatch/ru/common/v18/search?" + values.Encode()
}

func wildberriesSearchPageURL(query string) string {
	values := url.Values{}
	values.Set("search", query)
	return wildberriesHost + "/catalog/0/search.aspx?" + values.Encode()
}

func wildberriesRequestContext(ctx context.Context, referer string) context.Context {
	return shared.WithRequestHeaders(ctx, map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7",
		"Origin":          wildberriesHost,
		"Referer":         referer,
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "cross-site",
	})
}

func wildberriesDest(region string) string {
	return regions.WildberriesDest(region)
}

func decodeWildberriesJSON(page []byte, out any) error {
	if err := json.Unmarshal(page, out); err != nil {
		return fmt.Errorf("parse wildberries json: %w", err)
	}
	return nil
}
