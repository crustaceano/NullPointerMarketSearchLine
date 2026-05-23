package yandex

import (
	"strings"

	"nullpointer/backend/internal/adapters/shared"
)

func yandexBaseCharacteristics(region string) map[string]string {
	return map[string]string{
		"Регион":   region,
		"Источник": "Yandex Market",
	}
}

func absoluteYandexURL(href string) string {
	return shared.AbsoluteURL(yandexMarketHost, href)
}

func absoluteYandexAssetURL(src string) string {
	return shared.AbsoluteAssetURL(yandexMarketHost, src)
}

func normalizeYandexImageURL(src string) string {
	out := absoluteYandexAssetURL(src)
	if strings.Contains(out, "avatars.mds.yandex.net/get-mpic/") && strings.HasSuffix(out, "/") {
		return out + "orig"
	}
	return out
}
