"""FastAPI entrypoint for the lightweight ML normalization service."""

from pathlib import Path

from fastapi import FastAPI
from pydantic import BaseModel, Field

from normalizer import Dictionaries, Normalizer


BASE_DIR = Path(__file__).resolve().parent
DICT_DIR = BASE_DIR / "dictionaries"

dictionaries = Dictionaries.load(DICT_DIR)
normalizer = Normalizer(dictionaries)

app = FastAPI(title="NullPointer ML Normalizer", version="0.1.0")


class NormalizeRequest(BaseModel):
    query: str = Field(default="", description="Raw user query")


class NormalizeResponse(BaseModel):
    raw: str
    corrected: str
    synonyms: list[str]
    expanded_queries: list[str]


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "terms_loaded": len(dictionaries.terms),
        "synonym_keys": len(dictionaries.synonyms),
    }


@app.post("/normalize", response_model=NormalizeResponse)
def normalize(req: NormalizeRequest) -> NormalizeResponse:
    result = normalizer.normalize(req.query)
    return NormalizeResponse(**result)
