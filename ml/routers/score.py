"""POST /score — NLI-релевантность пары (query, products[]) (опционально)."""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from .deps import normalizer, scorer_enabled


router = APIRouter(tags=["score"])


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


@router.post("/score", response_model=ScoreResponse)
def score(req: ScoreRequest) -> ScoreResponse:
    """Скор релевантности (query, products[]) → [0, 1] через NLI.

    Скорит весь набор продуктов одним батчевым forward-pass.
    Ответ возвращает в исходном порядке `products` — клиент сам решает
    сортировку.
    """
    if not scorer_enabled():
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
