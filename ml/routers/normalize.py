"""POST /normalize — спеллчек, лемматизация и расширение запроса."""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel, Field

from .deps import normalizer


router = APIRouter(tags=["normalize"])


class NormalizeRequest(BaseModel):
    query: str = Field(default="", description="Raw user query")


class NormalizeResponse(BaseModel):
    raw: str
    corrected: str
    synonyms: list[str]
    expanded_queries: list[str]


@router.post("/normalize", response_model=NormalizeResponse)
def normalize(req: NormalizeRequest) -> NormalizeResponse:
    return NormalizeResponse(**normalizer.normalize(req.query))
