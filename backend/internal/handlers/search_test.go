package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSearchRequestNormalizesUnknownRegionToDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/search?query=ноутбук&region=ПАВп", nil)

	_, region, err := parseSearchRequest(req)
	if err != nil {
		t.Fatalf("parseSearchRequest() error = %v", err)
	}
	if region != defaultRegion {
		t.Fatalf("region = %q, want %q", region, defaultRegion)
	}
}

func TestParseSearchRequestNormalizesRegionAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/search?query=ноутбук&region=питер", nil)

	_, region, err := parseSearchRequest(req)
	if err != nil {
		t.Fatalf("parseSearchRequest() error = %v", err)
	}
	if region != "Санкт-Петербург" {
		t.Fatalf("region = %q, want %q", region, "Санкт-Петербург")
	}
}

func TestParseSearchRequestDefaultsEmptyRegion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/search?query=ноутбук", nil)

	_, region, err := parseSearchRequest(req)
	if err != nil {
		t.Fatalf("parseSearchRequest() error = %v", err)
	}
	if region != defaultRegion {
		t.Fatalf("region = %q, want %q", region, defaultRegion)
	}
}

func TestParseSearchRequestNormalizesPostRegion(t *testing.T) {
	req := httptest.NewRequest(
		http.MethodPost,
		"/search",
		strings.NewReader(`{"query":"ноутбук","region":"Nizhny Novgorod"}`),
	)

	_, region, err := parseSearchRequest(req)
	if err != nil {
		t.Fatalf("parseSearchRequest() error = %v", err)
	}
	if region != "Нижний Новгород" {
		t.Fatalf("region = %q, want %q", region, "Нижний Новгород")
	}
}

func TestParseSearchRequestNormalizesRegionWithoutYo(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/search?query=ноутбук&region=Могилев", nil)

	_, region, err := parseSearchRequest(req)
	if err != nil {
		t.Fatalf("parseSearchRequest() error = %v", err)
	}
	if region != "Могилёв" {
		t.Fatalf("region = %q, want %q", region, "Могилёв")
	}
}
