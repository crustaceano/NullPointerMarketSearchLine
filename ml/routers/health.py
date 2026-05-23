"""GET /health — статус нормализатора и опциональных подсистем."""

from __future__ import annotations

from fastapi import APIRouter

from .deps import gliner_enabled, normalizer, sage_enabled, scorer_enabled


router = APIRouter(tags=["health"])


@router.get("/health")
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
        "sage_enabled": sage_enabled(),
        "sage": normalizer.sage_name,
        "sage_loaded": normalizer.sage_loaded,
        "gliner_enabled": gliner_enabled(),
        "scorer_enabled": scorer_enabled(),
    }
