"""FastAPI entrypoint for the lightweight ML normalization service."""

import os
from pathlib import Path

from fastapi import FastAPI
from pydantic import BaseModel, Field

from entity_extractor import get_shared_entity_extractor
from normalizer import Dictionaries, Normalizer
from sage_corrector import get_shared_sage_corrector


BASE_DIR = Path(__file__).resolve().parent
DICT_DIR = BASE_DIR / "dictionaries"

dictionaries = Dictionaries.load(DICT_DIR)
sage = get_shared_sage_corrector()
extractor = get_shared_entity_extractor()
normalizer = Normalizer(dictionaries, sage=sage)

app = FastAPI(title="NullPointer ML Normalizer", version="0.8.0")


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


@app.get("/health")
def health() -> dict:
    sage_enabled = os.getenv("SAGE_ENABLED", "0").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )
    gliner_enabled = os.getenv("GLINER_ENABLED", "0").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )
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
        "sage_enabled": sage_enabled,
        "sage": normalizer.sage_name,
        "sage_loaded": normalizer.sage_loaded,
        "gliner_enabled": gliner_enabled,
        "gliner": extractor.name if extractor is not None else "disabled",
        "gliner_loaded": extractor.loaded if extractor is not None else False,
    }


@app.post("/normalize", response_model=NormalizeResponse)
def normalize(req: NormalizeRequest) -> NormalizeResponse:
    result = normalizer.normalize(req.query)
    return NormalizeResponse(**result)


@app.post("/extract", response_model=ExtractResponse)
def extract(req: ExtractRequest) -> ExtractResponse:
    raw = (req.query or "").strip()
    corrected = raw
    if req.use_corrected:
        corrected = str(normalizer.normalize(raw)["corrected"])
    if extractor is None:
        return ExtractResponse(raw=raw, corrected=corrected, entities={})
    return ExtractResponse(
        raw=raw,
        corrected=corrected,
        entities=extractor.extract(corrected, category=req.category),
    )
