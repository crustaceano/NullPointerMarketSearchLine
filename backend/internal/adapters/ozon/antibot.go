package ozon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
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
	l := launcher.New().
		Headless(true).
		NoSandbox(true).
		Append("disable-gpu", "").
		Append("disable-extensions", "").
		Append("disable-blink-features", "AutomationControlled")
	if browserBin := rodBrowserBin(); browserBin != "" {
		l = l.Bin(browserBin)
	}
	u := l.MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()

	return &SmartAntiCaptchaFetcher{
		baseFetcher: base,
		browser:     browser,
		cookies:     make(map[string][]*proto.NetworkCookie),
		userAgents:  make(map[string]string),
	}
}

func rodBrowserBin() string {
	for _, envName := range []string{"ROD_BROWSER_BIN", "CHROME_BIN", "CHROMIUM_BIN"} {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}

	return ""
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
		body, err := s.fetchWithBypassCredentials(ctx, host, rawURL, cachedCookies, cachedUA)
		if err == nil {
			return body, nil
		}
		if !errors.Is(err, ErrBlocked) {
			return nil, err
		}
		log.Printf("[SmartFetcher] Кэшированные куки для %s привели к капче, обновляем bypass-сессию", host)
		s.clearCachedBypass(host)
	}

	// 2. Блокировка параллельных запусков Chromium
	s.bypassMutex.Lock()
	defer s.bypassMutex.Unlock()

	s.mu.RLock()
	currentCookies := s.cookies[host]
	currentUA := s.userAgents[host]
	s.mu.RUnlock()

	if len(currentCookies) > 0 {
		body, err := s.fetchWithBypassCredentials(ctx, host, rawURL, currentCookies, currentUA)
		if err == nil {
			return body, nil
		}
		if !errors.Is(err, ErrBlocked) {
			return nil, err
		}
		log.Printf("[SmartFetcher] Обновленные другим потоком куки для %s тоже привели к капче, запускаем новый bypass", host)
		s.clearCachedBypass(host)
	}

	log.Printf("[SmartFetcher] Поток получил монопольный доступ. Запуск скрытого Chromium для %s...", host)

	newCookies, newUA, err := s.resolveCaptchaWithBrowser(rawURL)
	if err != nil {
		return nil, fmt.Errorf("browser captcha bypass failed: %w", err)
	}

	// Сохраняем куки в долговременную память бэкенда
	s.mu.Lock()
	s.cookies[host] = newCookies
	s.userAgents[host] = newUA
	s.mu.Unlock()

	body, err := s.fetchWithBypassCredentials(ctx, host, rawURL, newCookies, newUA)
	if err == nil {
		return body, nil
	}
	if errors.Is(err, ErrBlocked) {
		s.clearCachedBypass(host)
		return nil, fmt.Errorf("source still returned captcha after browser bypass")
	}
	return nil, err
}

func (s *SmartAntiCaptchaFetcher) fetchWithBypassCredentials(ctx context.Context, host, rawURL string, cookies []*proto.NetworkCookie, userAgent string) ([]byte, error) {
	retryCtx, cancel := s.contextWithBypassCredentials(ctx, host, cookies, userAgent)
	defer cancel()
	return s.baseFetcher.Fetch(retryCtx, rawURL)
}

func (s *SmartAntiCaptchaFetcher) clearCachedBypass(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cookies, host)
	delete(s.userAgents, host)
}

func (s *SmartAntiCaptchaFetcher) contextWithBypassCredentials(ctx context.Context, host string, cookies []*proto.NetworkCookie, userAgent string) (context.Context, context.CancelFunc) {
	cancel := func() {}
	if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
		log.Printf("[SmartFetcher] Контекст запроса истек для %s, выполняем ограниченный retry с кэшированными куками: %v", host, err)
		ctx, cancel = context.WithTimeout(context.Background(), s.baseFetcher.timeout)
	}

	ctx = context.WithValue(ctx, "bypass_cookies", cookies)
	ctx = context.WithValue(ctx, "bypass_ua", userAgent)
	return ctx, cancel
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
