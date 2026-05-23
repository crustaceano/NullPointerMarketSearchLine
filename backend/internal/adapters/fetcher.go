package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nullpointer/backend/internal/models"
)

const (
	defaultMaxBodyBytes = 5 * 1024 * 1024
	defaultUserAgent    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

// HTMLFetcher is the shared contract for HTML-based source parsers.
type HTMLFetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

type FetcherConfig struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	UserAgent    string
}

type DefaultHTMLFetcher struct {
	client       *http.Client
	maxBodyBytes int64
	userAgent    string
}

func NewHTMLFetcher(cfg FetcherConfig) *DefaultHTMLFetcher {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 6 * time.Second
	}

	maxBodyBytes := cfg.MaxBodyBytes
	if maxBodyBytes == 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &DefaultHTMLFetcher{
		client:       &http.Client{Timeout: timeout},
		maxBodyBytes: maxBodyBytes,
		userAgent:    userAgent,
	}
}

func (f *DefaultHTMLFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("empty url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.7")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch html: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, rawURL)
	}

	body, err := readLimited(resp.Body, f.maxBodyBytes)
	if err != nil {
		return nil, err
	}

	if looksBlocked(body) {
		return nil, errors.New("source returned anti-bot or captcha page")
	}

	return body, nil
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read html: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("html response is too large: limit is %d bytes", maxBytes)
	}
	return body, nil
}

func looksBlocked(body []byte) bool {
	text := strings.ToLower(string(body))
	blockMarkers := []string{
		"captcha",
		"капча",
		"robot",
		"are you human",
		"access denied",
		"доступ ограничен",
		"проверка безопасности",
	}

	for _, marker := range blockMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func limitOffers(offers []models.ProductOffer, limit int) []models.ProductOffer {
	if limit <= 0 || len(offers) <= limit {
		return offers
	}
	return offers[:limit]
}
