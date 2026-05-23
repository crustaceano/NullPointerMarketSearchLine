"""Data classes для query expansion pipeline (WordNet-based).

Старые `Concept` / `DetectedConcept` ушли вместе с concept-DB. Теперь
есть одна единица «расширяемого токена»: его текст, позиция, и список
синонимов из тезауруса. Список может быть пустым — тогда токен идёт
в выходной запрос как есть.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Literal


ProtectionKind = Literal["number", "size", "article"]


@dataclass(frozen=True)
class ProtectedSpan:
    """Кусок запроса, который нельзя трогать при расширении.

    `start` / `end` — позиции по словам (token-level), полуоткрытый интервал.
    После удаления brands/themes защищаются только regex-сущности:
    числа с единицами, размеры одежды, артикулы / SKU.
    """

    text: str
    kind: ProtectionKind
    start: int
    end: int


@dataclass(frozen=True)
class ExpandableToken:
    """Слово запроса + найденные WordNet-синонимы.

    Если `synonyms` пуст, expander просто оставляет слово на своём месте —
    значит, тезаурус про него ничего не знает (типичный случай для брендов,
    тем, доменного жаргона и компаундов).
    """

    text: str
    synonyms: tuple[str, ...]
    start: int
    end: int


@dataclass
class ParsedQuery:
    raw: str
    corrected: str
    tokens: list[str] = field(default_factory=list)
    expandable: list[ExpandableToken] = field(default_factory=list)
    protected_terms: list[ProtectedSpan] = field(default_factory=list)
    attributes: dict[str, str] = field(default_factory=dict)
