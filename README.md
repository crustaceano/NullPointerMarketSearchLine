# NullPointerMarketSearchLine

Baseline для Tender Hack — интеллектуальный поиск цен по маркетплейсам Рунета.

API-контракты между фронтендом, бэкендом и ML зафиксированы в [`API_CONTRACT.md`](API_CONTRACT.md).
Описание текущего Yandex Market парсера лежит в [`PARSER_YANDEX.md`](PARSER_YANDEX.md).

Стек:
- **Backend** — Go 1.26 + HTML/JSON-парсеры маркетплейсов.
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

## Как запустить локально

Рекомендуемый способ для команды — Docker Compose. Локальный запуск через
`run.sh` остаётся как fallback для разработки.

> **Важно:** нужны **два** процесса. ML на порту `8000` — это только нормализация запросов.
> Сайт и поиск открываются на **`http://127.0.0.1:8080`** (Go-бэкенд). Без Go страница не откроется.

### Docker Compose

Требуется Docker Desktop или Docker Engine с Compose plugin.

```bash
docker compose up --build
```

После запуска:

```text
Frontend + Go backend: http://127.0.0.1:8080
ML health-check:       http://127.0.0.1:8000/health
Backend health-check:  http://127.0.0.1:8080/health
```

Остановить:

```bash
docker compose down
```

ML-модели Hugging Face по умолчанию выключены (`SAGE_ENABLED=0`,
`GLINER_ENABLED=0`, `SCORER_ENABLED=0`, `LM_FILTER_ENABLED=0`). Если их
включить, кеш сохраняется в volume `ml-cache`.

### Локальный запуск без Docker

Потребуется **Go >= 1.22** и **Python >= 3.10**.

### Windows (два окна CMD или PowerShell)

**Окно 1** — ML:

```bat
cd ml
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
uvicorn app:app --host 127.0.0.1 --port 8000
```

Проверка: http://127.0.0.1:8000/health

**Окно 2** — Go (сначала установи Go: `winget install GoLang.Go`, перезапусти терминал):

```bat
cd backend
set ML_URL=http://127.0.0.1:8000
set WEB_DIR=web
go run .
```

Проверка: http://127.0.0.1:8080/health → в браузере http://127.0.0.1:8080

Если `go: command not found` — Go не в PATH, установи с https://go.dev/dl/ и **перезапусти терминал**.

### Linux / Git Bash

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

### ML: опциональный SAGE spell-checker

Локальная модель [ai-forever/sage-fredt5-distilled-95m](https://huggingface.co/ai-forever/sage-fredt5-distilled-95m) (~95M, без внешних API). По умолчанию **выключена** (быстрый старт).

```bash
cd ml
pip install -r requirements.txt   # torch + transformers при первом включении

export SAGE_ENABLED=1             # Git Bash / Linux
export SAGE_DEVICE=cpu            # или cuda, если есть GPU
uvicorn app:app --host 127.0.0.1 --port 8000
```

Первый запрос скачает веса с Hugging Face (~380 MB). В `/health`: `sage_enabled`, `sage_loaded`.

Когда SAGE включён: правит **весь запрос целиком**, SymSpell по токенам **не вызывается** (чтобы не портить уже исправленный текст).

### ML: опциональный GLiNER entity extractor

Извлекает структурированные сущности (товар, бренд, цвет, размер, сезон, индекс шин и т.д.) из запроса. По умолчанию **выключен**. Базовая модель [`urchade/gliner_multi-v2.1`](https://huggingface.co/urchade/gliner_multi-v2.1) (~209M).

```bash
cd ml
pip install -r requirements.txt

export GLINER_ENABLED=1
export GLINER_DEVICE=cpu                                  # или cuda
export GLINER_MODEL_ID=urchade/gliner_multi-v2.1          # дефолт
# либо локальный fine-tune:
# export GLINER_MODEL_ID=ml/models/gliner-marketplace
uvicorn app:app --host 127.0.0.1 --port 8000
```

Эндпоинт: `POST /extract` с телом `{"query": "...", "category": "одежда|шины|оргтехника"}`. Универсальные лейблы и наборы под категорию — в `ml/entity_extractor.py` (`DEFAULT_LABELS`, `LABELS_BY_CATEGORY`).

### ML: опциональный relevance scorer (NLI)

Считает скор релевантности пары **(текстовый запрос, JSON карточки товара)** в `[0, 1]`. Под капотом — модель NLI: для пары `(premise=карточка, hypothesis=запрос)` предсказывает три класса: `entailment / neutral / contradiction`, и собирает скор:

```
score = 0.5 + 0.5 * (P(entailment) - P(contradiction))
```

Свойства: `entailment=1 → 1.0`, `neutral=1 → 0.5`, `contradiction=1 → 0.0`. Так пары, которые **не противоречат** запросу, всегда получают больший скор, чем противоречивые.

Базовая модель — [`cointegrated/rubert-base-cased-nli-threeway`](https://huggingface.co/cointegrated/rubert-base-cased-nli-threeway) (~700 MB, RuBERT-base, обучена на русских NLI-датасетах). Карточки предполагаются на русском — multilingual mDeBERTa на CPU слишком медленная. По умолчанию **выключен**.

```bash
cd ml
pip install -r requirements.txt

export SCORER_ENABLED=1
export SCORER_DEVICE=cpu                                          # или cuda
export SCORER_MODEL_ID=cointegrated/rubert-base-cased-nli-threeway # дефолт
uvicorn app:app --host 127.0.0.1 --port 8000
```

Эндпоинт: `POST /score` — принимает один запрос и **набор** карточек, скорит их одним батчем.

```bash
curl -s -X POST localhost:8000/score \
  -H 'content-type: application/json' \
  -d '{
    "query": "летние шины 225/45 r17 michelin",
    "products": [
      {
        "title": "Летние шины Michelin Pilot Sport 4 225/45 R17 91W",
        "brand": "Michelin",
        "category": "Шины",
        "attributes": {"сезон": "летние", "размер": "225/45 R17"}
      },
      {
        "title": "Зимние шипованные Nokian Hakkapeliitta 235/55 R19",
        "brand": "Nokian",
        "category": "Шины"
      },
      {
        "title": "Принтер Brother HL-L2375",
        "brand": "Brother",
        "category": "Оргтехника"
      }
    ]
  }'
```

Ответ (порядок исходного `products` сохраняется — клиент сам сортирует если надо):
```json
{
  "raw": "...",
  "corrected": "...",
  "scored": [
    { "index": 0, "score": 0.92, "product_text": "Летние шины Michelin..." },
    { "index": 1, "score": 0.41, "product_text": "Зимние шипованные..." },
    { "index": 2, "score": 0.03, "product_text": "Принтер Brother..." }
  ]
}
```

Из карточки извлекаются `title`/`name`, `brand`, `category`, `description`, `attributes`/`specs`/`characteristics` (как dict, так и list). Лишние поля игнорируются. `use_corrected: true` (дефолт) сначала прогоняет запрос через нормализатор.

#### Бенч и эвал качества

Быстрый замер скорости (загрузит модель и прогонит несколько батчей):

```bash
python ml/scorer.py
```

Прогон на размеченных парах из `ml/data/scorer/golden.jsonl` (формат: `{group, query, product, label: "consistent"|"contradicts"}`):

```bash
python ml/eval_scorer.py                  # основной режим
python ml/eval_scorer.py --print-failures # покажет где скорер промахнулся
```

Метрики:
- `mean_consistent` / `mean_contradicts` — средние скоры по классам, разница между ними = `separation`.
- `pairwise ranking acc` — главная метрика: внутри одной группы (один query, несколько карточек) для каждой пары `(consistent, contradicts)` проверяем, что `score(consistent) > score(contradicts)`. Это и есть «четкость сортировки».
- `ROC-AUC` — глобальная по всему датасету.
- `binary acc` — accuracy при пороге (по умолчанию 0.5).

Размечать новые пары — просто дописывайте строки в `golden.jsonl`. Группы с одинаковым `query` склеиваются автоматически.

### ML: controlled query expansion (`POST /expand`)

Поверх «концептной» базы синонимов: detect → protect → expand → validate. На выходе — несколько поисковых вариантов одного запроса, в которых меняются **только** доменные слова, а бренды / темы / числа / размеры остаются дословно.

База — три JSON-файла (правятся руками):
- `ml/data/synonyms/concepts.json` — концепты с `phrases` (узнаём) + `expansions` (подставляем);
- `ml/data/synonyms/brands.json` — бренды (защита от изменений), кириллица + латиница;
- `ml/data/synonyms/themes.json` — франшизы / тематики (`one piece`, `май литл пони`).

Пайплайн в `ml/synonyms/`:
```
raw → tokenize
    → ProtectedSpanFinder  (brand / theme / number / size / article)
    → DomainTermDetector   (exact → fuzzy → embedding)
    → ParsedQuery
    → generate_expanded_queries (variant-at-a-time)
    → validate_expanded_query
```

Эндпоинт всегда включён, тяжёлых моделей не требует:

```bash
curl -s -X POST localhost:8000/expand \
  -H 'content-type: application/json' \
  -d '{"query": "Зипка пурпурная май литл пони", "max_queries": 5}' | jq
```

Ответ:
```json
{
  "raw": "Зипка пурпурная май литл пони",
  "corrected": "зипка пурпурная май литл пони",
  "detected_concepts": [
    { "raw_phrase": "зипка",     "concept_id": "zip_hoodie",   "method": "exact", "confidence": 1.0 },
    { "raw_phrase": "пурпурная", "concept_id": "color_purple", "method": "exact", "confidence": 1.0 }
  ],
  "protected_terms": [{ "text": "май литл пони", "kind": "theme" }],
  "attributes": { "color": "фиолетовый" },
  "unknown_terms": [],
  "expanded_queries": [
    { "query": "зипка пурпурная май литл пони",                "valid": true },
    { "query": "зип худи пурпурная май литл пони",             "valid": true },
    { "query": "толстовка на молнии пурпурная май литл пони",  "valid": true },
    { "query": "худи на молнии пурпурная май литл пони",       "valid": true },
    { "query": "зипка фиолетовый май литл пони",               "valid": true }
  ]
}
```

Поля `attributes` (color / size / brand / model) удобно прокидывать в фильтры маркетплейса. `valid: false` приходит когда после расширения потерялся защищённый span или весь концепт — такие варианты клиент может отбрасывать.

#### Опциональный embedding-fallback

Если детектор не нашёл концепт через exact и fuzzy (`мастерка`, `олимпийка`, мульти-словные перифразы), включается семантический матч через multilingual MiniLM. Модель тяжёлая (~470 MB), по умолчанию **выключена**.

```bash
export EMBED_ENABLED=1
export EMBED_DEVICE=cpu                                                            # или cuda
export EMBED_MODEL_ID=sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2  # дефолт
uvicorn app:app --host 127.0.0.1 --port 8000
```

При старте `routers/deps.py` загрузит модель и проиндексирует все `concept.phrases` (~80 фраз, индекс ≈ 120 KB в RAM). Дальше каждый детект делает один `encode` для непокрытого n-gram и матричный cosine с предкэшированными векторами. Порог по умолчанию 0.65.

В `DetectedConcept.method` появится значение `"embedding"` — удобно отличать «строгий» матч от семантического.

#### Известные ограничения (план Day-3)

- **SymSpell-коррекция в /expand отключена** (`apply_typo_correction=False` в `routers/deps.py`): на редких доменных словах (`зимная` → `земная`) она ошибается. Будет включена обратно, когда добавим лемматизацию через `pymorphy3` и SymSpell-boost для `concept.phrases`.
- **Skip-grams через защищённые токены пока не поддерживаются**: запрос вроде `принтер хп лазерный` не схлопывается в `printer_laser`, потому что бренд между токенами разрывает n-gram окно.
- **Unknown-terms log + candidate-flow** (полуавтоматическое расширение базы) — следующий итерационный шаг.

### SymSpell: словарь частот

Основной русский словарь: `ml/data/ru-100k.txt` (формат: `слово частота`, ~100k строк).  
Доменные бусты: `ml/dictionaries/*.json` (футболка, принтер, шины…).  
Если `ru-100k.txt` удалить — fallback на `wordfreq`.

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
