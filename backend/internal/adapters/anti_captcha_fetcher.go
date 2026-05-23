package adapters

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

type SmartAntiCaptchaFetcher struct {
	baseFetcher *DefaultHTMLFetcher
	browser     *rod.Browser
	mu          sync.RWMutex
	cookies     map[string]string
	userAgents  map[string]string
}

func NewSmartAntiCaptchaFetcher(base *DefaultHTMLFetcher) *SmartAntiCaptchaFetcher {
	u := launcher.New().
		Headless(true).
		NoSandbox(true).
		Append("disable-gpu", "").
		Append("disable-extensions", "").
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()

	return &SmartAntiCaptchaFetcher{
		baseFetcher: base,
		browser:     browser,
		cookies:     make(map[string]string),
		userAgents:  make(map[string]string),
	}
}

func (s *SmartAntiCaptchaFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	host := parsedURL.Host

	s.mu.RLock()
	cachedCookie := s.cookies[host]
	cachedUA := s.userAgents[host]
	s.mu.RUnlock()

	fastCtx := ctx
	if cachedCookie != "" {
		fastCtx = context.WithValue(fastCtx, "bypass_cookies", cachedCookie)
		fastCtx = context.WithValue(fastCtx, "bypass_ua", cachedUA)
	}

	body, err := s.baseFetcher.Fetch(fastCtx, rawURL)
	if err == nil {
		return body, nil
	}

	log.Printf("[SmartFetcher] Блокировка или капча на %s. Запуск Chromium...", host)

	newCookie, newUA, err := s.resolveCaptchaWithBrowser(rawURL)
	if err != nil {
		return nil, fmt.Errorf("browser captcha bypass failed: %w", err)
	}

	s.mu.Lock()
	s.cookies[host] = newCookie
	s.userAgents[host] = newUA
	s.mu.Unlock()

	retryCtx := context.WithValue(ctx, "bypass_cookies", newCookie)
	retryCtx = context.WithValue(retryCtx, "bypass_ua", newUA)

	return s.baseFetcher.Fetch(retryCtx, rawURL)
}

func (s *SmartAntiCaptchaFetcher) resolveCaptchaWithBrowser(targetURL string) (string, string, error) {
	// 1. stealth.Page принимает инстанс браузера (s.browser) и сам создает маскированную страницу
	// Для инкогнито используем s.browser.MustIncognito(), чтобы сессии не пересекались
	page, err := stealth.Page(s.browser.MustIncognito())
	if err != nil {
		return "", "", fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer page.MustClose()

	// Оптимизация сетевого трафика (блокируем тяжелые медиаресурсы)
	router := page.HijackRequests()
	router.MustAdd("*.jpg", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	router.MustAdd("*.png", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	router.MustAdd("*.woff*", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	go router.Run()
	defer router.Stop()

	// Переход на маркетплейс с ограничением времени
	err = page.Timeout(15 * time.Second).Navigate(targetURL)
	if err != nil {
		return "", "", err
	}
	page.MustWaitIdle()

	// Вытаскиваем куки обхода капчи
	rodCookies, err := page.Cookies(nil)
	if err != nil {
		return "", "", err
	}

	var cookieStr string
	for _, c := range rodCookies {
		cookieStr += c.Name + "=" + c.Value + "; "
	}

	// Извлекаем сгенерированный Stealth User-Agent
	uaEval, err := page.Eval(`() => navigator.userAgent`)
	if err != nil {
		return "", "", err
	}

	return cookieStr, uaEval.Value.Str(), nil
}
