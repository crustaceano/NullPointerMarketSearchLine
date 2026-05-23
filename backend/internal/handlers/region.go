package handlers

import "nullpointer/backend/internal/regions"

const defaultRegion = regions.Default

func normalizeRegion(raw string) string {
	return regions.Normalize(raw)
}
