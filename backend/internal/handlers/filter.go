package handlers

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"nullpointer/backend/internal/models"
)

var characteristicNumberRe = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)?`)

var relevanceStopwords = map[string]struct{}{
	"для": {},
	"или": {},
	"под": {},
	"при": {},
	"без": {},
	"с":   {},
	"и":   {},
	"в":   {},
	"на":  {},
	"по":  {},
}

type scoredOffer struct {
	offer models.ProductOffer
	score int
}

func filterRelevantOffers(offers []models.ProductOffer, norm models.Normalization) []models.ProductOffer {
	if len(offers) == 0 {
		return offers
	}

	primary := queryTokens(norm.Corrected)
	if len(primary) == 0 {
		primary = queryTokens(norm.Raw)
	}
	synonyms := queryTokens(strings.Join(norm.Synonyms, " "))

	if len(primary) == 0 && len(synonyms) == 0 {
		return offers
	}

	scored := make([]scoredOffer, 0, len(offers))
	for _, offer := range offers {
		score := relevanceScore(offer, primary, synonyms)
		if score > 0 {
			score += offerQualityScore(offer)
			if score > 0 {
				scored = append(scored, scoredOffer{offer: offer, score: score})
			}
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	filtered := make([]models.ProductOffer, 0, len(scored))
	for _, item := range scored {
		filtered = append(filtered, item.offer)
	}
	return filtered
}

func offerQualityScore(offer models.ProductOffer) int {
	chars := offer.Characteristics
	if len(chars) == 0 {
		return 0
	}

	score := 0
	availability := characteristicValue(chars, "налич")
	switch {
	case containsAny(availability, "нет", "раскуп", "законч", "распрод"):
		score -= 10
	case containsAny(availability, "под заказ"):
		score -= 3
	case containsAny(availability, "да", "есть", "в наличии"):
		score += 5
	}

	delivery := characteristicValue(chars, "достав")
	if containsAny(delivery, "сегодня", "завтра") {
		score += 3
	} else if delivery != "" {
		score += 1
	}

	if rating := parseCharacteristicFloat(characteristicValue(chars, "рейтинг")); rating >= 4.5 {
		score += 4
	} else if rating >= 4.0 {
		score += 2
	}

	if reviews := parseCharacteristicInt(characteristicValue(chars, "отзыв")); reviews >= 100 {
		score += 3
	} else if reviews >= 20 {
		score += 1
	}

	return score
}

func characteristicValue(chars map[string]string, keyPart string) string {
	keyPart = strings.ToLower(keyPart)
	for key, value := range chars {
		if strings.Contains(strings.ToLower(key), keyPart) {
			return strings.ToLower(value)
		}
	}
	return ""
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func parseCharacteristicFloat(text string) float64 {
	match := characteristicNumberRe.FindString(text)
	if match == "" {
		return 0
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(match, ",", "."), 64)
	if err != nil {
		return 0
	}
	return value
}

func parseCharacteristicInt(text string) int {
	match := characteristicNumberRe.FindString(text)
	if match == "" {
		return 0
	}
	normalized := strings.ReplaceAll(match, ",", ".")
	value, err := strconv.Atoi(strings.Split(normalized, ".")[0])
	if err != nil {
		return 0
	}
	return value
}

func relevanceScore(offer models.ProductOffer, primary, synonyms []string) int {
	haystack := strings.ToLower(offer.Title + " " + characteristicsText(offer.Characteristics))
	score := 0

	for _, token := range primary {
		if strings.Contains(haystack, token) {
			score += 3
		}
	}
	for _, token := range synonyms {
		if strings.Contains(haystack, token) {
			score++
		}
	}
	return score
}

func queryTokens(text string) []string {
	seen := map[string]struct{}{}
	var tokens []string

	for _, token := range splitSearchTokens(text) {
		if len([]rune(token)) < 3 {
			continue
		}
		if _, stop := relevanceStopwords[token]; stop {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func splitSearchTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

func characteristicsText(chars map[string]string) string {
	if len(chars) == 0 {
		return ""
	}
	parts := make([]string, 0, len(chars)*2)
	for key, value := range chars {
		parts = append(parts, key, value)
	}
	return strings.Join(parts, " ")
}
