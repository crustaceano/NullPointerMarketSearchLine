"""Регексп-защита токенов, которые нельзя подменять синонимами.

После отказа от concept-DB здесь остались только структурные сущности,
которые в принципе не должны иметь синонимов в маркетплейсе:
  * `size`    — размеры одежды (`xs`, `xxl`, `42`, `42.5`).
  * `number`  — числовые токены с возможной единицей (`107w`, `225/45`,
                `512gb`, `4060ti`).
  * `article` — артикулы/SKU: ≥2 латинских + ≥2 цифр в любом порядке
                (`hl-l2375dwr`, `pilot-sport-4`).

Бренды (`michelin`, `мишлен`) и темы (`naruto`, `май литл пони`) теперь
не помечаются — они остаются «обычными» словами; полагаемся на то, что
RuWordNet их не знает (имена собственные не в тезаурусе) и поэтому
expansion для них не сгенерится.
"""

from __future__ import annotations

import re

from .schema import ProtectedSpan


_NUMBER_UNIT_RE = re.compile(
    r"^\d+(?:[./x]\d+)*[a-z]*$",  # 205, 205/55, 4k, 107w, 512gb, 2.5x
    re.IGNORECASE,
)
_ARTICLE_RE = re.compile(
    r"^(?=[\w-]*[a-z])(?=[\w-]*\d)[a-z0-9-]{3,}$",
    re.IGNORECASE,
)
_CLOTHING_SIZE_RE = re.compile(
    r"^(?:xx?s|xx?l|xxx?l|s|m|l|xs|xl)$|^\d{2}(?:\.\d)?$",
    re.IGNORECASE,
)


class ProtectedSpanFinder:
    """Stateless regex-only protector. Не требует ни БД, ни конфигов."""

    def find(self, tokens: list[str]) -> list[ProtectedSpan]:
        spans: list[ProtectedSpan] = []
        for i, tok in enumerate(tokens):
            kind = self._classify_token(tok)
            if kind is not None:
                spans.append(
                    ProtectedSpan(text=tok, kind=kind, start=i, end=i + 1)
                )
        return spans

    @staticmethod
    def _classify_token(tok: str) -> str | None:
        if _CLOTHING_SIZE_RE.match(tok):
            return "size"
        if _NUMBER_UNIT_RE.match(tok):
            return "number"
        if _ARTICLE_RE.match(tok):
            return "article"
        return None
