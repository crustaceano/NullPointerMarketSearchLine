"""FastAPI entrypoint for the lightweight ML normalization service."""

import os
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from normalizer import Dictionaries, Normalizer
from sage_corrector import get_shared_sage_corrector


BASE_DIR = Path(__file__).resolve().parent
DICT_DIR = BASE_DIR / "dictionaries"

dictionaries = Dictionaries.load(DICT_DIR)
sage = get_shared_sage_corrector()
normalizer = Normalizer(dictionaries, sage=sage)

app = FastAPI(title="NullPointer ML Normalizer", version="0.8.0")


def _flag(name: str) -> bool:
    return os.getenv(name, "0").strip().lower() in ("1", "true", "yes", "on")


def _gliner_enabled() -> bool:
    return _flag("GLINER_ENABLED")


def _scorer_enabled() -> bool:
    return _flag("SCORER_ENABLED")


class NormalizeRequest(BaseModel):
    query: str = Field(default="", description="Raw user query")


class NormalizeResponse(BaseModel):
    raw: str
    corrected: str
    synonyms: list[str]
    expanded_queries: list[str]


class ExtractRequest(BaseModel):
    query: str = Field(default="", description="Текст для извлечения сущностей")
    use_corrected: bool = Field(
        default=True,
        description="Сначала прогнать запрос через нормализатор",
    )
    category: str | None = Field(
        default=None,
        description=(
            "Опциональная категория (одежда / шины / оргтехника). "
            "Если указана — берётся узкий набор лейблов под неё."
        ),
    )


class ExtractResponse(BaseModel):
    raw: str
    corrected: str
    entities: dict[str, list[str]]


class ScoreRequest(BaseModel):
    query: str = Field(..., description="Текстовый запрос пользователя")
    products: list[dict[str, Any]] = Field(
        ...,
        description=(
            "Набор JSON карточек товара. Поддерживаемые поля у каждого: "
            "title/name, brand, category, description, "
            "attributes/specs/characteristics. Лишние поля игнорируются."
        ),
    )
    use_corrected: bool = Field(
        default=True,
        description="Сначала прогнать запрос через нормализатор",
    )


class ScoredProduct(BaseModel):
    index: int = Field(..., description="Позиция в исходном `products`")
    score: float = Field(..., description="Скор релевантности в [0, 1]")
    product_text: str = Field(
        ..., description="Плоский текст карточки, скормленный модели"
    )


class ScoreResponse(BaseModel):
    raw: str
    corrected: str
    scored: list[ScoredProduct]


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "domain_terms": normalizer.domain_terms_count,
        "synonym_keys": normalizer.synonym_keys_count,
        "stopwords_filter": normalizer.filter_stopwords,
        "stopwords_loaded": len(normalizer.stopwords),
        "full_vocab": normalizer.full_vocab_size,
        "freq_dict": "ml/data/ru-100k.txt",
        "corrector": normalizer.corrector_name,
        "lemmatizer": normalizer.lemmatizer_name,
        "sage_enabled": _flag("SAGE_ENABLED"),
        "sage": normalizer.sage_name,
        "sage_loaded": normalizer.sage_loaded,
        "gliner_enabled": _gliner_enabled(),
        "scorer_enabled": _scorer_enabled(),
    }


@app.post("/normalize", response_model=NormalizeResponse)
def normalize(req: NormalizeRequest) -> NormalizeResponse:
    result = normalizer.normalize(req.query)
    return NormalizeResponse(**result)


@app.post("/extract", response_model=ExtractResponse)
def extract(req: ExtractRequest) -> ExtractResponse:
    if not _gliner_enabled():
        raise HTTPException(
            status_code=503,
            detail=(
                "GLiNER entity extraction отключен. "
                "Установи GLINER_ENABLED=1 (опционально GLINER_MODEL_ID=...) "
                "перед запуском сервиса."
            ),
        )

    # Импорт лениво — чтобы при выключенном GLiNER в проде не тянуть
    # gliner/torch и не есть RAM на инициализации.
    from entity_extractor import get_shared_entity_extractor

    extractor = get_shared_entity_extractor()
    if extractor is None:
        raise HTTPException(status_code=503, detail="GLiNER не инициализирован")

    raw = (req.query or "").strip()
    corrected = raw
    if req.use_corrected:
        corrected = str(normalizer.normalize(raw)["corrected"])
    return ExtractResponse(
        raw=raw,
        corrected=corrected,
        entities=extractor.extract(corrected, category=req.category),
    )


@app.post("/score", response_model=ScoreResponse)
def score(req: ScoreRequest) -> ScoreResponse:
    """Скор релевантности (query, products[]) → [0, 1] через cross-encoder.

    Скорит весь набор продуктов одним батчевым forward-pass.
    Ответ возвращает в исходном порядке `products` — клиент сам решает
    сортировку.
    """
    if not _scorer_enabled():
        raise HTTPException(
            status_code=503,
            detail=(
                "Relevance scorer отключен. "
                "Установи SCORER_ENABLED=1 (опционально SCORER_MODEL_ID=...) "
                "перед запуском сервиса."
            ),
        )

    if not req.products:
        raise HTTPException(
            status_code=400, detail="`products` не может быть пустым"
        )

    # Импорт лениво — torch + transformers нужны только при включенном scorer.
    from scorer import get_shared_scorer, product_to_text

    scorer = get_shared_scorer()
    if scorer is None:
        raise HTTPException(status_code=503, detail="Scorer не инициализирован")

    raw = (req.query or "").strip()
    corrected = raw
    if req.use_corrected:
        corrected = str(normalizer.normalize(raw)["corrected"])

    product_texts = [product_to_text(p) for p in req.products]

    # Скорим только непустые карточки одним батчем; пустые получают score=0.
    payload_idx = [i for i, t in enumerate(product_texts) if t]
    payload_texts = [product_texts[i] for i in payload_idx]

    values_by_idx: dict[int, float] = {}
    if payload_texts:
        values = scorer.score_many(corrected, payload_texts)
        if values is None:
            raise HTTPException(
                status_code=500, detail="Scorer inference failed"
            )
        values_by_idx = dict(zip(payload_idx, values))

    scored = [
        ScoredProduct(
            index=i,
            score=values_by_idx.get(i, 0.0),
            product_text=text,
        )
        for i, text in enumerate(product_texts)
    ]

    return ScoreResponse(raw=raw, corrected=corrected, scored=scored)
