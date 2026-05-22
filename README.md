# NullPointerMarketSearchLine

Baseline для Tender Hack — интеллектуальный поиск цен по маркетплейсам Рунета.

Стек:
- **Backend** — Go 1.22+ (стандартная библиотека, без внешних зависимостей).
- **ML / нормализация запросов** — Python 3.10+ + FastAPI (локально, без внешних API).
- **Frontend** — статический HTML + ванильный JavaScript, отдаётся из Go.

## Архитектура

```
┌──────────┐      HTTP        ┌──────────────┐     HTTP      ┌────────────────┐
│ Браузер  │ ───────────────▶ │ Go backend   │ ────────────▶ │ Python ML      │
│ (HTML+JS)│ ◀─────────────── │ :8080        │ ◀──────────── │ FastAPI :8000  │
└──────────┘    JSON          └──────┬───────┘    JSON       └────────────────┘
                                     │
                                     │ параллельно
                ┌────────────────────┼────────────────────┐
                ▼                    ▼                    ▼
        Yandex Market         Ozon / WB           Citilink (non-marketplace)
        (mock adapter)        (mock adapter)      (mock adapter)
```

Поток запроса:

1. Фронтенд шлёт `GET /search?query=...&region=...` в Go-бэкенд.
2. Go-бэкенд вызывает Python-сервис `POST /normalize` — получает `corrected`, `synonyms`, `expanded_queries`.
3. Go параллельно опрашивает все адаптеры источников (горутины + WaitGroup). Ошибка одного источника не ломает другие — возвращается `status: "error"`.
4. Ответ собирается в единый `SearchResponse` и отдаётся фронту, который группирует офферы по источнику.

Все адаптеры реализуют интерфейс `SourceAdapter`, так что моки можно заменить реальными HTTP+HTML парсерами не трогая остальной код.

## Структура проекта

```
NullPointerMarketSearchLine/
├── README.md
├── run.sh                          # запускает ML + Go разом (Linux/macOS/Git Bash)
├── .gitignore
├── backend/                        # Go service
│   ├── go.mod
│   ├── main.go
│   ├── internal/
│   │   ├── models/product.go       # ProductOffer, SourceResult, SearchResponse, Normalization
│   │   ├── normalizer/client.go    # HTTP-клиент к Python ML
│   │   ├── adapters/
│   │   │   ├── adapter.go          # интерфейс SourceAdapter + All()
│   │   │   ├── mock.go             # общий mock-генератор офферов
│   │   │   ├── yandex.go
│   │   │   ├── ozon.go
│   │   │   ├── wildberries.go
│   │   │   └── runet.go            # сменный non-marketplace источник (Citilink по умолчанию)
│   │   └── handlers/
│   │       ├── health.go           # GET /health
│   │       └── search.go           # GET|POST /search
│   └── web/                        # статический фронтенд
│       ├── index.html
│       ├── styles.css
│       └── app.js
└── ml/                             # Python ML service
    ├── requirements.txt
    ├── app.py                      # FastAPI: /health, /normalize
    ├── normalizer.py               # Levenshtein + словари
    └── dictionaries/
        ├── clothing.json
        ├── tires.json
        └── office.json
```

## Как запустить локально (Linux)

Потребуется `go >= 1.22` и `python3 >= 3.10`.

### Вариант 1 — одной командой

```bash
chmod +x run.sh
./run.sh
```

Скрипт поднимет ML-сервис на `:8000` и Go-бэкенд на `:8080`.

### Вариант 2 — вручную, два терминала

Терминал 1 — ML:

```bash
cd ml
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app:app --host 0.0.0.0 --port 8000
```

Терминал 2 — Go-бэкенд:

```bash
cd backend
go run .
```

Открой [http://localhost:8080](http://localhost:8080) — это веб-интерфейс.

### Переменные окружения

| Переменная | По умолчанию              | Назначение                       |
|------------|---------------------------|----------------------------------|
| `ADDR`     | `:8080`                   | На каком адресе слушает Go       |
| `ML_URL`   | `http://localhost:8000`   | Адрес Python ML-сервиса          |
| `WEB_DIR`  | `web`                     | Папка со статикой фронтенда      |

## Пример запроса

```bash
curl "http://localhost:8080/search?query=футбока%20хлопок&region=Москва"
```

Эквивалент через POST:

```bash
curl -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query":"футбока хлопок","region":"Москва"}'
```

### Пример ответа (сокращённо)

```json
{
  "query": "футбока хлопок",
  "region": "Москва",
  "normalization": {
    "raw": "футбока хлопок",
    "corrected": "футболка хлопок",
    "synonyms": ["майка", "тишотка", "t-shirt"],
    "expanded_queries": [
      "футболка хлопок",
      "майка хлопок",
      "тишотка хлопок"
    ]
  },
  "sources": [
    {
      "source": "Yandex Market",
      "status": "success",
      "offers": [
        {
          "source": "Yandex Market",
          "title": "Футболка Хлопок — базовая модель",
          "image": "https://placehold.co/240x180?text=Yandex+Market+1",
          "price": 4990,
          "currency": "RUB",
          "url": "https://market.yandex.ru/search?text=...",
          "characteristics": {
            "Регион": "Москва",
            "Гарантия": "12 мес.",
            "В наличии": "да"
          }
        }
      ]
    },
    { "source": "Ozon",        "status": "success", "offers": [/* ... */] },
    { "source": "Wildberries", "status": "success", "offers": [/* ... */] },
    { "source": "Citilink",    "status": "success", "offers": [/* ... */] }
  ]
}
```

Эндпоинт `GET /health` возвращает:

```json
{ "status": "ok", "ml": "ok" }
```

Если Python-сервис не поднят — Go-бэкенд продолжает работать, `corrected` будет равен `raw`, а `ml` в `/health` станет `unavailable`. UI всё равно отрисует результаты адаптеров.

## Что дальше (после baseline)

1. **Реальные парсеры** вместо моков:
   - HTTP-клиент с ротацией User-Agent + прокси.
   - HTML-парсинг через `goquery` (`github.com/PuerkitoBio/goquery`).
   - Отдельные парсеры для каталога / поисковой выдачи каждого источника.
2. **Лёгкая кеш-прослойка** (in-memory LRU по ключу `query+region+source`) — снизить нагрузку и время ответа.
3. **Расширение ML**:
   - заменить кастомный Levenshtein на `rapidfuzz`;
   - добавить эмбеддинги (`fasttext` или `sentence-transformers` маленькой моделью) для семантического матчинга;
   - выделить классификатор категории запроса (clothing / tires / office / …) — пока используется всё словари сразу.
4. **Ранжирование и дедупликация** офферов между источниками (нормализация title, fuzzy-match SKU).
5. **Региональность** — пробросить регион в реальные URL-параметры (Ozon `from_global=true`, WB `dest=`, Yandex Market `lr=`).
6. **Тесты**:
   - Go: table-driven по адаптерам и хендлерам;
   - Python: pytest на `Normalizer.normalize` с фикстурами опечаток.
7. **Метрики** — `/metrics` Prometheus, время на источник, доля ошибок.
8. **Docker / docker-compose** для одной команды деплоя.
