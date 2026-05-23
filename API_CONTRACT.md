# API-контракт

Документ фиксирует текущие MVP-контракты между фронтендом, Go-бэкендом и
Python ML-сервисом.

## Поток запроса

Основной поиск товаров всегда идёт через бэкенд:

```text
Frontend -> Go Backend -> Python ML
                    |
                    +-> Source adapters
                    |
Frontend <- Go Backend
```

Фронтенд не вызывает ML-сервис напрямую в основном сценарии поиска. Бэкенд
отвечает за оркестрацию, fallback нормализации, параллельный запуск адаптеров,
обработку ошибок и группировку ответа по источникам.

## Frontend -> Backend

### GET /search

Используется текущим веб-интерфейсом.

Query parameters:

```text
query:  string, required
region: string, optional, default "Москва"
```

Пример:

```http
GET /search?query=ноутубук&region=Москва
```

### POST /search

Эквивалентен `GET /search`, но принимает JSON.

Request:

```json
{
  "query": "ноутубук",
  "region": "Москва"
}
```

Валидация:

```text
query is required
region defaults to "Москва" when empty
unsupported region values are normalized to "Москва" in MVP
```

Поддержанные регионы MVP:

```text
Москва
Санкт-Петербург
Вологда
Волгоград
Казань
Новосибирск
Екатеринбург
Нижний Новгород
Краснодар
Ростов-на-Дону
Самара
Уфа
Челябинск
Омск
Пермь
Воронеж
Саратов
Тюмень
Ижевск
Барнаул
Владивосток
Ярославль
Тула
Рязань
Калуга
Тверь
Смоленск
Курск
Белгород
Минск
Могилёв
```

Региональное поведение источников:

```text
Yandex Market: регион пробрасывается в HTML search URL через lr.
Wildberries: регион пробрасывается в search URL через dest.
Ozon: регион нормализуется и передаётся в карточки; search URL строится как HTML-поиск с from_global=true.
```

Для Ozon точная установка города обычно требует гео-сессии/кук или внутренних
эндпоинтов сайта. В MVP это не делается, чтобы не добавлять отдельный API-вызов
для геолокации.

Error response:

```json
{
  "error": "query is required"
}
```

## Backend -> ML

### POST /normalize

Бэкенд отправляет в ML сырой запрос, полученный от фронтенда.

Request:

```json
{
  "query": "ноутубук"
}
```

Response:

```json
{
  "raw": "ноутубук",
  "corrected": "ноутбук",
  "synonyms": ["лэптоп", "laptop", "notebook"],
  "expanded_queries": ["ноутбук", "лэптоп", "laptop", "notebook"]
}
```

Fallback-поведение бэкенда, если ML недоступен или вернул некорректный ответ:

```json
{
  "raw": "ноутубук",
  "corrected": "ноутубук",
  "synonyms": [],
  "expanded_queries": ["ноутубук"]
}
```

Бэкенд ищет товары по `normalization.corrected`. Если `corrected` пустой,
используется исходный запрос фронтенда.

## Backend -> Frontend

### SearchResponse

Тело ответа для `GET /search` и `POST /search`.

```json
{
  "query": "ноутубук",
  "region": "Москва",
  "normalization": {
    "raw": "ноутубук",
    "corrected": "ноутбук",
    "synonyms": ["лэптоп", "laptop", "notebook"],
    "expanded_queries": ["ноутбук", "лэптоп", "laptop", "notebook"]
  },
  "sources": [
    {
      "source": "Yandex Market",
      "status": "success",
      "offers": [
        {
          "source": "Yandex Market",
          "title": "Ноутбук Example 15",
          "image": "https://example.com/image.jpg",
          "price": 52990,
          "currency": "RUB",
          "url": "https://market.yandex.ru/product/example",
          "characteristics": {
            "Регион": "Москва",
            "Источник": "Yandex Market"
          }
        }
      ]
    }
  ]
}
```

## Статус источника

Каждый источник возвращается независимо. Ошибка одного адаптера не должна ломать
весь ответ `/search`.

```text
success: адаптер вернул один или больше офферов
empty:   адаптер завершился без ошибки, но не вернул офферы
error:   адаптер завершился с ошибкой; причина лежит в поле error
```

Пример ошибки:

```json
{
  "source": "Yandex Market",
  "status": "error",
  "error": "yandex market returned no parsable offers",
  "offers": []
}
```

Пример пустого результата:

```json
{
  "source": "Yandex Market",
  "status": "empty",
  "offers": []
}
```

## Health Checks

### Backend: GET /health

Response:

```json
{
  "status": "ok",
  "ml": "ok"
}
```

Если ML недоступен:

```json
{
  "status": "ok",
  "ml": "unavailable"
}
```

### ML: GET /health

Бэкенд ожидает HTTP 200 от ML-сервиса. Тело ответа принадлежит ML-сервису и
бэкендом не используется.

## Контракт парсера

Каждый адаптер источника реализует:

```go
Search(ctx context.Context, query string, region string) ([]models.ProductOffer, error)
```

Вход адаптера:

```text
query:  нормализованный запрос из ML, обычно normalization.corrected
region: регион из frontend request или default "Москва"
```

Выход адаптера:

```text
return offers, nil       -> status источника success или empty
return nil/offers, error -> status источника error
```

Обязательные поля оффера для успешного результата парсера:

```text
source
title
image
price
currency
url
characteristics
```
