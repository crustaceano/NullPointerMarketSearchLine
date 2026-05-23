package handlers

import (
	"testing"

	"nullpointer/backend/internal/models"
)

func TestFilterRelevantOffersKeepsMatchingTitle(t *testing.T) {
	offers := []models.ProductOffer{
		{Title: "Ноутбук Lenovo IdeaPad", Characteristics: map[string]string{"Регион": "Москва"}},
		{Title: "Смартфон Samsung", Characteristics: map[string]string{"Регион": "Москва"}},
	}
	norm := models.Normalization{Raw: "ноутубук", Corrected: "ноутбук"}

	filtered := filterRelevantOffers(offers, norm)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Title != "Ноутбук Lenovo IdeaPad" {
		t.Fatalf("filtered[0].Title = %q", filtered[0].Title)
	}
}

func TestFilterRelevantOffersUsesSynonymsAndCharacteristics(t *testing.T) {
	offers := []models.ProductOffer{
		{
			Title: "Apple MacBook Air 13",
			Characteristics: map[string]string{
				"Тип": "laptop",
			},
		},
		{Title: "Apple iPhone 15"},
	}
	norm := models.Normalization{
		Raw:       "ноутубук",
		Corrected: "ноутбук",
		Synonyms:  []string{"laptop", "notebook"},
	}

	filtered := filterRelevantOffers(offers, norm)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Title != "Apple MacBook Air 13" {
		t.Fatalf("filtered[0].Title = %q", filtered[0].Title)
	}
}

func TestFilterRelevantOffersRanksByQualityCharacteristics(t *testing.T) {
	offers := []models.ProductOffer{
		{
			Title: "Ноутбук Basic",
			Characteristics: map[string]string{
				"В наличии": "да",
			},
		},
		{
			Title: "Ноутбук Pro",
			Characteristics: map[string]string{
				"В наличии": "да",
				"Рейтинг":   "4.8",
				"Отзывы":    "145",
				"Доставка":  "доставка завтра",
			},
		},
	}
	norm := models.Normalization{Raw: "ноутбук", Corrected: "ноутбук"}

	filtered := filterRelevantOffers(offers, norm)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	if filtered[0].Title != "Ноутбук Pro" {
		t.Fatalf("filtered[0].Title = %q", filtered[0].Title)
	}
}

func TestFilterRelevantOffersDropsClearlyUnavailableOffers(t *testing.T) {
	offers := []models.ProductOffer{
		{
			Title: "Ноутбук Old",
			Characteristics: map[string]string{
				"В наличии": "нет",
			},
		},
		{
			Title: "Ноутбук New",
			Characteristics: map[string]string{
				"В наличии": "да",
			},
		},
	}
	norm := models.Normalization{Raw: "ноутбук", Corrected: "ноутбук"}

	filtered := filterRelevantOffers(offers, norm)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Title != "Ноутбук New" {
		t.Fatalf("filtered[0].Title = %q", filtered[0].Title)
	}
}
