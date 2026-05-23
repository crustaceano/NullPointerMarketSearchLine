"""Раскладывает raw query → токены + protected + WordNet-синонимы.

Pipeline (упрощённый, без concept-DB):
  raw → optional typo correction (Normalizer/SymSpell)
      → tokenize
      → ProtectedSpanFinder (regex: числа/артикулы/размеры)
      → lemmatize (pymorphy3) → WordNetLookup
      → ParsedQuery

Зачем лемматизация: RuWordNet хранит синсеты по леммам. Без неё
`шины` / `красная` / `летние` ничего не находят (есть только `шина`
/ `красный` / `летний`). Lookup делаем по лемме, а в `ExpandableToken`
оставляем оригинальную поверхностную форму — expander заменит именно
её на синоним.

`parse_dual` сохраняет старую семантику: можем параллельно прогнать
и raw-ветку, и typo-ветку (если SymSpell что-то поменял), чтобы потом
объединить кандидатов и не зависеть от одной интерпретации запроса.
"""

from __future__ import annotations

import re
from typing import Any

from .protector import ProtectedSpanFinder
from .schema import ExpandableToken, ParsedQuery, ProtectedSpan
from .wordnet_lookup import WordNetLookup


_TOKEN_RE = re.compile(r"[\w\-/]+", flags=re.UNICODE)


def _tokenize(text: str) -> list[str]:
    return [m.group(0) for m in _TOKEN_RE.finditer(text)]


class QueryParser:
    def __init__(
        self,
        wordnet: WordNetLookup,
        protector: ProtectedSpanFinder | None = None,
        normalizer: Any | None = None,
        lemmatizer: Any | None = None,
        apply_typo_correction: bool = False,
    ) -> None:
        self.wordnet = wordnet
        self.protector = protector or ProtectedSpanFinder()
        self.normalizer = normalizer
        # Optional Lemmatizer (см. ml/morphology.py). Если не передали,
        # пробуем достать из normalizer.lemmatizer (там pymorphy3 уже
        # инициализирован под domain-vocab).
        if lemmatizer is None and normalizer is not None:
            lemmatizer = getattr(normalizer, "lemmatizer", None)
        self.lemmatizer = lemmatizer
        self.apply_typo_correction = apply_typo_correction

    def parse(self, raw: str) -> ParsedQuery:
        return self._parse(raw, apply_typo=self.apply_typo_correction)

    def parse_dual(
        self, raw: str
    ) -> tuple[ParsedQuery, ParsedQuery | None]:
        """`(raw_branch, typo_branch_or_None)`.

        typo-ветка появляется только когда normalizer реально что-то
        поменял. Это даёт два набора кандидатов: «как пользователь набрал»
        и «как поправил SymSpell» — выше по pipeline'у их объединяем.
        """
        parsed_raw = self._parse(raw, apply_typo=False)
        if self.normalizer is None:
            return parsed_raw, None
        parsed_typo = self._parse(raw, apply_typo=True)
        if parsed_typo.corrected.strip().lower() == parsed_raw.corrected.strip().lower():
            return parsed_raw, None
        return parsed_raw, parsed_typo

    def _parse(self, raw: str, apply_typo: bool) -> ParsedQuery:
        raw_clean = (raw or "").strip()

        corrected = raw_clean
        if apply_typo and self.normalizer is not None and raw_clean:
            corrected = str(self.normalizer.normalize(raw_clean)["corrected"])

        tokens = _tokenize(corrected.lower())

        protected = self.protector.find(tokens)
        protected_mask = [False] * len(tokens)
        for span in protected:
            for i in range(span.start, span.end):
                if 0 <= i < len(protected_mask):
                    protected_mask[i] = True

        expandable: list[ExpandableToken] = []
        for i, tok in enumerate(tokens):
            if protected_mask[i]:
                continue
            synonyms = self._lookup_with_lemma(tok)
            expandable.append(
                ExpandableToken(
                    text=tok,
                    synonyms=tuple(synonyms),
                    start=i,
                    end=i + 1,
                )
            )

        attributes = self._extract_attributes(protected)

        return ParsedQuery(
            raw=raw_clean,
            corrected=corrected,
            tokens=tokens,
            expandable=expandable,
            protected_terms=protected,
            attributes=attributes,
        )

    def _lookup_with_lemma(self, token: str) -> list[str]:
        """WordNet lookup с предварительной лемматизацией.

        Стратегия: сначала пробуем лемму (даёт основной recall), потом
        fallback на исходную форму на случай если лемматизатор сменил
        слово в редкое (типа `липучки` → `липучка` — обе ок). Дедуп.
        Если лемматизатора нет — просто исходная форма.
        """
        results: list[str] = []
        seen: set[str] = set()

        if self.lemmatizer is not None:
            try:
                lemma = self.lemmatizer.lemma(token)
            except Exception:
                lemma = token
            if lemma:
                for s in self.wordnet.lookup(lemma):
                    if s not in seen and s != token:
                        seen.add(s)
                        results.append(s)

        if not results or self.lemmatizer is None:
            for s in self.wordnet.lookup(token):
                if s not in seen and s != token:
                    seen.add(s)
                    results.append(s)

        return results

    @staticmethod
    def _extract_attributes(
        protected: list[ProtectedSpan],
    ) -> dict[str, str]:
        attrs: dict[str, str] = {}
        for p in protected:
            if p.kind == "size" and "size" not in attrs:
                attrs["size"] = p.text
            elif p.kind == "article" and "model" not in attrs:
                attrs["model"] = p.text
        return attrs
