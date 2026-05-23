package adapters

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"nullpointer/backend/internal/adapters/shared"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestLooksBlockedDoesNotMatchGenericRobotWord(t *testing.T) {
	body := []byte(`<html><body><script>window.robotConfig = {}</script></body></html>`)

	if looksBlocked(body) {
		t.Fatal("looksBlocked() matched generic robot word")
	}
}

func TestLooksBlockedMatchesCaptchaPage(t *testing.T) {
	body := []byte(`<html><body>Подтвердите, что вы не робот</body></html>`)

	if !looksBlocked(body) {
		t.Fatal("looksBlocked() did not match captcha page")
	}
}

func TestDefaultHTMLFetcherAppliesRequestHeadersFromContext(t *testing.T) {
	var gotAccept, gotCustom string

	fetcher := &DefaultHTMLFetcher{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAccept = req.Header.Get("Accept")
			gotCustom = req.Header.Get("X-Test-Header")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     http.Header{},
				Request:    req,
			}, nil
		})},
		maxBodyBytes: defaultMaxBodyBytes,
		userAgent:    defaultUserAgent,
	}

	ctx := shared.WithRequestHeaders(context.Background(), map[string]string{
		"Accept":        "application/json, text/plain, */*",
		"X-Test-Header": "wildberries",
	})

	if _, err := fetcher.Fetch(ctx, "https://example.test/search"); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotAccept != "application/json, text/plain, */*" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotCustom != "wildberries" {
		t.Fatalf("X-Test-Header = %q", gotCustom)
	}
}
