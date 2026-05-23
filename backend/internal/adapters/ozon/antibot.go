package ozon

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
	cookies     map[string][]*proto.NetworkCookie
	userAgents  map[string]string
	bypassMutex sync.Mutex
}

func NewSmartAntiCaptchaFetcher(base *DefaultHTMLFetcher) *SmartAntiCaptchaFetcher {
	u := launcher.New().
		Headless(true).
		NoSandbox(true).
		Append("disable-gpu", "").
		Append("disable-extensions", "").
		Append("disable-blink-features", "AutomationControlled").
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()

	return &SmartAntiCaptchaFetcher{
		baseFetcher: base,
		browser:     browser,
		cookies:     make(map[string][]*proto.NetworkCookie),
		userAgents:  make(map[string]string),
	}
}

func (s *SmartAntiCaptchaFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	host := parsedURL.Host

	// 1. Проверяем кэш кук
	s.mu.RLock()
	cachedCookies := s.cookies[host]
	cachedUA := s.userAgents[host]
	s.mu.RUnlock()

	if len(cachedCookies) > 0 {
		fastCtx := context.WithValue(ctx, "bypass_cookies", cachedCookies)
		fastCtx = context.WithValue(fastCtx, "bypass_ua", cachedUA)
		return s.baseFetcher.Fetch(fastCtx, rawURL)
	}

	// 2. Блокировка параллельных запусков Chromium
	s.bypassMutex.Lock()

	s.mu.RLock()
	currentCookies := s.cookies[host]
	currentUA := s.userAgents[host]
	s.mu.RUnlock()

	if len(currentCookies) > 0 {
		s.bypassMutex.Unlock()
		retryCtx := context.WithValue(ctx, "bypass_cookies", currentCookies)
		retryCtx = context.WithValue(retryCtx, "bypass_ua", currentUA)
		return s.baseFetcher.Fetch(retryCtx, rawURL)
	}

	log.Printf("[SmartFetcher] Поток получил монопольный доступ. Запуск скрытого Chromium для %s...", host)

	bgCtx, bgCancel := context.WithTimeout(context.Background(), 45*time.Second)
	_ = bgCtx // используем для логики таймаута внутри resolveCaptchaWithBrowser

	newCookies, newUA, err := s.resolveCaptchaWithBrowser(rawURL)
	bgCancel()

	if err != nil {
		s.bypassMutex.Unlock()
		return nil, fmt.Errorf("browser captcha bypass failed: %w", err)
	}

	// Сохраняем куки в долговременную память бэкенда
	s.mu.Lock()
	s.cookies[host] = newCookies
	s.userAgents[host] = newUA
	s.mu.Unlock()

	s.bypassMutex.Unlock()

	// 3. Если к этому моменту контекст самого HTTP-запроса пользователя (curl) ещё жив —
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("куки успешно получены в фоне, но клиент оборвал соединение по таймауту: %w", ctx.Err())
	default:
	}

	retryCtx := context.WithValue(ctx, "bypass_cookies", newCookies)
	retryCtx = context.WithValue(retryCtx, "bypass_ua", newUA)
	return s.baseFetcher.Fetch(retryCtx, rawURL)
}

func (s *SmartAntiCaptchaFetcher) resolveCaptchaWithBrowser(targetURL string) ([]*proto.NetworkCookie, string, error) {
	page, err := stealth.Page(s.browser.MustIncognito())
	if err != nil {
		return nil, "", fmt.Errorf("failed to create stealth page: %w", err)
	}
	defer page.MustClose()

	router := page.HijackRequests()
	router.MustAdd("*.jpg", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	router.MustAdd("*.png", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	router.MustAdd("*.woff*", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
	go router.Run()
	defer router.Stop()

	// Даем браузеру до 30 секунд внутри на полную загрузку страницы и прохождение проверок
	err = page.Timeout(30 * time.Second).Navigate(targetURL)
	if err != nil {
		return nil, "", err
	}
	page.MustWaitIdle()

	// Небольшая пауза, чтобы куки успели осесть в сессии Chrome
	time.Sleep(3 * time.Second)

	rodCookies, err := page.Cookies(nil)
	if err != nil {
		return nil, "", err
	}

	uaEval, err := page.Eval(`() => navigator.userAgent`)
	if err != nil {
		return nil, "", err
	}

	return rodCookies, uaEval.Value.Str(), nil
}
