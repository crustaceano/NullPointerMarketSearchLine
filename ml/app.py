"""FastAPI entrypoint for the lightweight ML normalization service."""

import os
from pathlib import Path

from fastapi import FastAPI
from pydantic import BaseModel, Field

from normalizer import Dictionaries, Normalizer
from sage_corrector import get_shared_sage_corrector


BASE_DIR = Path(__file__).resolve().parent
DICT_DIR = BASE_DIR / "dictionaries"

dictionaries = Dictionaries.load(DICT_DIR)
sage = get_shared_sage_corrector()
normalizer = Normalizer(dictionaries, sage=sage)

app = FastAPI(title="NullPointer ML Normalizer", version="0.7.0")


class NormalizeRequest(BaseModel):
    query: str = Field(default="", description="Raw user query")


class NormalizeResponse(BaseModel):
    raw: str
    corrected: str
    synonyms: list[str]
    expanded_queries: list[str]


@app.get("/health")
def health() -> dict:
    sage_enabled = os.getenv("SAGE_ENABLED", "0").strip().lower() in (
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
    }


@app.post("/normalize", response_model=NormalizeResponse)
def normalize(req: NormalizeRequest) -> NormalizeResponse:
    result = normalizer.normalize(req.query)
    return NormalizeResponse(**result)
