package regions

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":                Default,
		"ПАВп":            Default,
		" мск ":           "Москва",
		"питер":           "Санкт-Петербург",
		"Nizhny Novgorod": "Нижний Новгород",
		"Могилев":         "Могилёв",
	}

	for raw, want := range cases {
		if got := Normalize(raw); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestMarketplaceRegionCodes(t *testing.T) {
	if got := YandexLR("Санкт-Петербург"); got != "2" {
		t.Fatalf("YandexLR() = %q", got)
	}
	if got := WildberriesDest("Могилёв"); got != "-82606" {
		t.Fatalf("WildberriesDest() = %q", got)
	}
}
