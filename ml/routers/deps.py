"""Shared dependencies для FastAPI-роутеров.

Здесь живут синглтоны, которые дёшево создать на старте:
  * `Normalizer` (с словарями + опциональным SAGE-корректором);
  * `WordNetLookup` (lazy: реальный коннект к sqlite БД RuWordNet
    откладывается до первого `lookup`);
  * `QueryParser` поверх обоих.

Тяжёлые модели (NLI scorer, GLiNER) тянутся только когда соответствующий
эндпоинт реально позовут — через ENV-флаги.
"""

from __future__ import annotations

import os
from pathlib import Path

from normalizer import Dictionaries, Normalizer
from sage_corrector import get_shared_sage_corrector
from synonyms import (
    ProtectedSpanFinder,
    QueryParser,
    get_shared_wordnet,
)


BASE_DIR = Path(__file__).resolve().parent.parent
DICT_DIR = BASE_DIR / "dictionaries"


def flag(name: str, default: bool = False) -> bool:
    """`true/1/yes/on` (case-insensitive) → True, иначе → False.

    `default` используется когда ENV-переменная вообще не задана.
    """
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def gliner_enabled() -> bool:
    return flag("GLINER_ENABLED")


def sage_enabled() -> bool:
    return flag("SAGE_ENABLED")


def scorer_enabled() -> bool:
    """NLI-scorer для /score (релевантность товара запросу).

    В /expand используется отдельный LM-фильтр (см. lm_filter_enabled).
    Дефолт = выкл, чтобы зря не грузить 700 MB модель если /score не
    дёргают.
    """
    return flag("SCORER_ENABLED", default=False)


def lm_filter_enabled() -> bool:
    """LM-perplexity фильтр expanded queries в /expand.

    Включён по умолчанию: лёгкая модель (~50 MB), грузится один раз,
    отсекает грамматический/семантический бред после WordNet-замены
    (`баллон зимняя с шипами` и подобное). Отключить: LM_FILTER_ENABLED=0.
    """
    return flag("LM_FILTER_ENABLED", default=True)


dictionaries = Dictionaries.load(DICT_DIR)
sage = get_shared_sage_corrector()
normalizer = Normalizer(dictionaries, sage=sage)

wordnet = get_shared_wordnet()

query_parser = QueryParser(
    wordnet=wordnet,
    protector=ProtectedSpanFinder(),
    normalizer=normalizer,
    # Дефолт для одиночного parse(): SymSpell применяется. В /expand
    # parse_dual() всё равно прогоняет обе ветки независимо: raw=без typo,
    # typo_corrected=с typo. Этот флаг влияет только на прямые parse(raw)
    # вызовы (например, если из других модулей используют parser).
    apply_typo_correction=True,
)
