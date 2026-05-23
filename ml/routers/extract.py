"""POST /extract — извлечение сущностей через GLiNER (опционально)."""

from __future__ import annotations

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from .deps import gliner_enabled, normalizer


router = APIRouter(tags=["extract"])


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


@router.post("/extract", response_model=ExtractResponse)
def extract(req: ExtractRequest) -> ExtractResponse:
    if not gliner_enabled():
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
