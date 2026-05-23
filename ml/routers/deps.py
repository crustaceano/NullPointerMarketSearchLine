"""Shared dependencies for routers.

Сюда вынесли:
  * единственный на процесс инстанс `Normalizer` (с подгруженными словарями
    и опциональным SAGE-корректором);
  * helper'ы для чтения булевых ENV-флагов опциональных тяжёлых фич
    (GLINER_ENABLED, SCORER_ENABLED).

Импортируется один раз — благодаря этому FastAPI запускается с уже
готовым нормализатором, а ленивые ML-модули (GLiNER, scorer) не
тянутся, пока соответствующий эндпоинт реально не позовут.
"""

from __future__ import annotations

import os
from pathlib import Path

from normalizer import Dictionaries, Normalizer
from sage_corrector import get_shared_sage_corrector


BASE_DIR = Path(__file__).resolve().parent.parent
DICT_DIR = BASE_DIR / "dictionaries"

dictionaries = Dictionaries.load(DICT_DIR)
sage = get_shared_sage_corrector()
normalizer = Normalizer(dictionaries, sage=sage)


def flag(name: str) -> bool:
    """`true/1/yes/on` (case-insensitive) → True, всё остальное → False."""
    return os.getenv(name, "0").strip().lower() in ("1", "true", "yes", "on")


def gliner_enabled() -> bool:
    return flag("GLINER_ENABLED")


def sage_enabled() -> bool:
    return flag("SAGE_ENABLED")


def scorer_enabled() -> bool:
    return flag("SCORER_ENABLED")
