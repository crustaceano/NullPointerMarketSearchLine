package ozon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/go-rod/rod/lib/proto"

	"nullpointer/backend/internal/models"
)

type HTMLFetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

type FetcherConfig struct {
	Timeout time.Duration
}

type DefaultHTMLFetcher struct {
	allocatorCtx context.Context
	cancel       context.CancelFunc
	timeout      time.Duration
}

func NewHTMLFetcher(cfg FetcherConfig) *DefaultHTMLFetcher {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)

	allocatorCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)

	return &DefaultHTMLFetcher{
		allocatorCtx: allocatorCtx,
		cancel:       cancel,
		timeout:      timeout,
	}
}

func (f *DefaultHTMLFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("empty url")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	timeoutCtx, cancelTimeout := context.WithTimeout(f.allocatorCtx, f.timeout)
	defer cancelTimeout()

	browserCtx, cancelBrowser := chromedp.NewContext(timeoutCtx)
	defer cancelBrowser()

	var htmlString string

	actions := []chromedp.Action{
		network.Enable(),
	}

	// 1. Читаем User Agent
	userAgentStr := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	if cachedUA, ok := ctx.Value("bypass_ua").(string); ok && cachedUA != "" {
		userAgentStr = cachedUA
	}

	actions = append(actions, network.SetExtraHTTPHeaders(network.Headers{
		"Accept-Language": "ru-RU,ru;q=0.9,en;q=0.7",
		"User-Agent":      userAgentStr,
	}))

	// 2. 🔥 БЕЗОПАСНЫЙ ИСПРАВЛЕННЫЙ ПЕРЕНОС КУК:
	// Принимаем массив структур []*proto.NetworkCookie
	if rodCookies, ok := ctx.Value("bypass_cookies").([]*proto.NetworkCookie); ok && len(rodCookies) > 0 {
		for _, rc := range rodCookies {
			// Локальная копия для замыкания в цикле Go
			cookie := rc

			actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
				// Заполняем параметры SetCookie с ювелирной точностью, соответствующей CDP протоколу
				cmd := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain(cookie.Domain).
					WithPath(cookie.Path).
					WithHTTPOnly(cookie.HTTPOnly).
					WithSecure(cookie.Secure)

				return cmd.Do(ctx)
			}))
		}
	}

	// Сценарий получения данных
	actions = append(actions,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`window.scrollBy(0, 1200)`, nil),
		chromedp.Sleep(2*time.Second),
		chromedp.OuterHTML("html", &htmlString, chromedp.ByQuery),
	)

	err := chromedp.Run(browserCtx, actions...)
	if err != nil {
		return nil, fmt.Errorf("chromedp fetch %s failed: %w", rawURL, err)
	}

	body := []byte(htmlString)

	if looksBlocked(body) {
		return nil, errors.New("source returned anti-bot or captcha page")
	}

	return body, nil
}

func looksBlocked(body []byte) bool {
	text := strings.ToLower(string(body))

	blockMarkers := []string{
		"captcha__form",
		"/showcaptcha",
		"/checkcaptcha",
		"подтвердите, что вы не робот",
		"докажите, что вы не робот",
		"are you human",
		"access denied",
		"доступ ограничен",
		"проверка безопасности",
		"too many requests",
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
