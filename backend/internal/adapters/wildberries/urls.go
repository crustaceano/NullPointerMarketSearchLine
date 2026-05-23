package wildberries

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func wildberriesProductURL(id int64) string {
	return fmt.Sprintf("%s/catalog/%d/detail.aspx", wildberriesHost, id)
}

func wildberriesCardURL(id int64) string {
	vol := id / 100000
	part := id / 1000
	return fmt.Sprintf("https://basket-%s.wbbasket.ru/vol%d/part%d/%d/info/ru/card.json", wildberriesBasket(vol), vol, part, id)
}

func wildberriesImageURL(id int64) string {
	vol := id / 100000
	part := id / 1000
	return fmt.Sprintf("https://basket-%s.wbbasket.ru/vol%d/part%d/%d/images/c516x688/1.webp", wildberriesBasket(vol), vol, part, id)
}

func wildberriesIDFromProductURL(rawURL string) int64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "catalog" {
			continue
		}
		id, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err == nil {
			return id
		}
	}
	return 0
}

func wildberriesBasket(vol int64) string {
	switch {
	case vol <= 143:
		return "01"
	case vol <= 287:
		return "02"
	case vol <= 431:
		return "03"
	case vol <= 719:
		return "04"
	case vol <= 1007:
		return "05"
	case vol <= 1061:
		return "06"
	case vol <= 1115:
		return "07"
	case vol <= 1169:
		return "08"
	case vol <= 1313:
		return "09"
	case vol <= 1601:
		return "10"
	case vol <= 1655:
		return "11"
	case vol <= 1919:
		return "12"
	case vol <= 2045:
		return "13"
	case vol <= 2189:
		return "14"
	case vol <= 2405:
		return "15"
	case vol <= 2621:
		return "16"
	case vol <= 2837:
		return "17"
	case vol <= 3053:
		return "18"
	case vol <= 3269:
		return "19"
	case vol <= 3485:
		return "20"
	case vol <= 3701:
		return "21"
	case vol <= 3917:
		return "22"
	case vol <= 4133:
		return "23"
	case vol <= 4349:
		return "24"
	case vol <= 4565:
		return "25"
	case vol <= 4877:
		return "26"
	case vol <= 5061:
		return "27"
	case vol <= 5485:
		return "28"
	case vol <= 5805:
		return "29"
	case vol <= 6061:
		return "30"
	case vol <= 6377:
		return "31"
	case vol <= 6597:
		return "32"
	case vol <= 6861:
		return "33"
	case vol <= 7101:
		return "34"
	case vol <= 7613:
		return "35"
	case vol <= 8017:
		return "36"
	case vol <= 8413:
		return "37"
	case vol <= 8817:
		return "38"
	case vol <= 9257:
		return "39"
	case vol <= 9681:
		return "40"
	case vol <= 10085:
		return "41"
	case vol <= 10489:
		return "42"
	case vol <= 10893:
		return "43"
	case vol <= 11297:
		return "44"
	case vol <= 11697:
		return "45"
	default:
		return "46"
	}
}
