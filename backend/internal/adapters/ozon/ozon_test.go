package ozon

import (
	"strings"
	"testing"
)

func TestBuildOzonSearchURLUsesNormalizedRegion(t *testing.T) {
	url := buildOzonSearchURL(" ноутбук ", "питер")

	if !strings.HasPrefix(url, "https://www.ozon.ru/search/?") {
		t.Fatalf("url = %s", url)
	}
	if !strings.Contains(url, "text=%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA") {
		t.Fatalf("url does not contain escaped query: %s", url)
	}
	if !strings.Contains(url, "region=%D0%A1%D0%B0%D0%BD%D0%BA%D1%82-%D0%9F%D0%B5%D1%82%D0%B5%D1%80%D0%B1%D1%83%D1%80%D0%B3") {
		t.Fatalf("url does not contain normalized region: %s", url)
	}
	if !strings.Contains(url, "from_global=true") {
		t.Fatalf("url does not contain from_global=true: %s", url)
	}
}
