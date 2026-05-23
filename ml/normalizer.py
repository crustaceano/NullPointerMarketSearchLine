"""Lightweight query normalizer (typo-correction only).

Pipeline:
  raw → [optional SAGE full-sentence cleanup] →
  tokenize → [drop stopwords only if STOPWORDS_FILTER=1] → [SymSpell].

When SAGE_ENABLED=1, SymSpell per-token step is skipped (SAGE already fixed text).

Synonym expansion здесь **не делается**. Единственный источник синонимов
в системе — WordNet (см. /expand pipeline в `synonyms/`). Поля
`synonyms` в `dictionaries/*.json` остаются исключительно для domain-boost
SymSpell: слова, перечисленные в `terms` и `synonyms`, защищены от
ошибочной коррекции в общерусское слово (например, `мфу`, `tshirt`,
`сканер`).

Two vocabularies in play:
- domain_vocab : project-specific anchor words from JSON dictionaries. A token
  in domain_vocab is kept exactly so user-facing terms ("кроссовки", "шины")
  aren't lemmatized into less search-friendly singulars. Also gives SymSpell
  domain bias so "приттер" lands on "принтер", not "питер".
- full_vocab   : ml/data/ru-100k.txt (+ domain boost from JSON). Fed to SymSpell.

Everything is local; deps (symspellpy / pymorphy3 / wordfreq / stop-words)
are optional and degrade gracefully.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Dict, List, Optional, Set

from corrector import Corrector, build_corrector
from morphology import Lemmatizer, build_lemmatizer
from sage_corrector import SageCorrector
from vocabulary import (
    build_general_vocab,
    build_stopwords,
    merge_domain,
    stopwords_filter_enabled,
)


TOKEN_RE = re.compile(r"[\w\-]+", flags=re.UNICODE)


@dataclass
class Dictionaries:
    """Domain-specific terms + synonyms loaded from JSON files."""

    terms: Set[str] = field(default_factory=set)
    synonyms: Dict[str, List[str]] = field(default_factory=dict)

    @classmethod
    def load(cls, folder: Path) -> "Dictionaries":
        terms: Set[str] = set()
        synonyms: Dict[str, List[str]] = {}
        for path in sorted(folder.glob("*.json")):
            with path.open("r", encoding="utf-8") as f:
                data = json.load(f)
            for t in data.get("terms", []):
                terms.add(t.lower())
            for key, values in data.get("synonyms", {}).items():
                key_l = key.lower()
                synonyms.setdefault(key_l, [])
                for v in values:
                    v_l = v.lower()
                    if v_l not in synonyms[key_l]:
                        synonyms[key_l].append(v_l)
        return cls(terms=terms, synonyms=synonyms)


def _tokenize(text: str) -> List[str]:
    return [m.group(0).lower() for m in TOKEN_RE.finditer(text)]


class Normalizer:
    """Stateless query normalizer."""

    def __init__(
        self,
        dicts: Dictionaries,
        max_expansions: int = 6,
        corrector: Optional[Corrector] = None,
        lemmatizer: Optional[Lemmatizer] = None,
        sage: Optional[SageCorrector] = None,
        general_vocab_size: int = 100_000,
    ) -> None:
        self.dicts = dicts
        self.max_expansions = max_expansions

        # Domain vocab: project anchors we keep as-is (canonical search forms).
        domain_words: Set[str] = set(dicts.terms)
        domain_words.update(dicts.synonyms.keys())
        for syns in dicts.synonyms.values():
            domain_words.update(syns)
        self._domain_vocab: Set[str] = {w.lower() for w in domain_words}

        # Full vocab for SymSpell = ru-100k.txt + domain boost from JSON.
        full_vocab_freqs = build_general_vocab(top_n=general_vocab_size)
        merge_domain(
            full_vocab_freqs,
            domain_terms=dicts.terms,
            domain_synonyms={s for syns in dicts.synonyms.values() for s in syns}
            | set(dicts.synonyms.keys()),
        )
        self._full_vocab_size = len(full_vocab_freqs)
        self._full_vocab: Set[str] = set(full_vocab_freqs.keys())

        # Extra guard: if a word is common Russian (zipf >= threshold),
        # treat it as already valid and do not auto-correct it.
        self._zipf_frequency: Optional[Callable[[str, str], float]] = None
        try:
            from wordfreq import zipf_frequency  # optional
            self._zipf_frequency = zipf_frequency
        except Exception:
            self._zipf_frequency = None

        self.stopwords: Set[str] = build_stopwords()
        self.filter_stopwords: bool = stopwords_filter_enabled()

        if corrector is None:
            corrector, _ = build_corrector(full_vocab_freqs)
        self.corrector = corrector

        if lemmatizer is None:
            lemmatizer = build_lemmatizer()
        lemmatizer.set_vocab(self._domain_vocab)
        self.lemmatizer = lemmatizer

        self._sage = sage

    @property
    def sage_name(self) -> str:
        if self._sage is None:
            return "disabled"
        return getattr(self._sage, "name", "sage")

    @property
    def sage_loaded(self) -> bool:
        return bool(self._sage and getattr(self._sage, "loaded", False))

    @property
    def domain_terms_count(self) -> int:
        return len(self.dicts.terms)

    @property
    def synonym_keys_count(self) -> int:
        return len(self.dicts.synonyms)

    @property
    def corrector_name(self) -> str:
        return getattr(self.corrector, "name", "unknown")

    @property
    def lemmatizer_name(self) -> str:
        return getattr(self.lemmatizer, "name", "unknown")

    @property
    def full_vocab_size(self) -> int:
        return self._full_vocab_size

    def add_anchor_words(self, words) -> None:
        """Помечает слова как «канонические» — не корректировать опечатками.

        Используется когда поверх Normalizer-а живёт synonym DB с
        концептами: их single-word phrases должны попадать в детектор
        в неискажённом виде, иначе SymSpell может перепутать редкое
        доменное слово (`зипка`, `мастерка`) с похожим из общего русского.
        """
        for w in words:
            w_l = (w or "").strip().lower()
            if w_l and " " not in w_l:
                self._domain_vocab.add(w_l)

    def _normalize_token(self, token: str) -> str:
        """Resolve a token to its best searchable form."""
        # Keep sizes/articles/models unchanged: 15, 42, x5, 4060ti.
        if not any(ch.isalpha() for ch in token):
            return token

        # Conservative mode: do not "fix" already valid words.
        # 1) domain word (project anchors / jargon dict)
        # 2) known by built vocabulary (ru-100k.txt)
        # 3) reasonably common Russian word by zipf_frequency
        if token in self._domain_vocab or token in self._full_vocab:
            return token
        if self._zipf_frequency is not None and self._zipf_frequency(token, "ru") >= 1.5:
            # Понизили порог 2.0 → 1.5: больше «редких но реальных» слов
            # пройдут как valid. Цена — мы можем пропустить опечатку, но
            # это лучше чем подменить редкое доменное слово (`зипка`,
            # `мфу`, `тишотка`) на похожее общерусское.
            return token

        corrected_raw = self.corrector.correct(token)
        if corrected_raw == token:
            return token

        # Anchor: коррекция в доменное слово — всегда полезна
        # (`приттер` → `принтер`).
        if corrected_raw in self._domain_vocab:
            return corrected_raw

        # Guard: коррекция должна явно поднимать частотность.
        # Иначе мы подменяем одно редкое слово на другое (`зипка`→`липка`),
        # а пользователь хотел именно redкое доменное.
        if self._zipf_frequency is not None:
            z_token = self._zipf_frequency(token, "ru")
            z_corr = self._zipf_frequency(corrected_raw, "ru")
            # требуем минимум +1.0 zipf (≈ 10× частотнее) — разница между
            # «совсем редкое» и «вполне нормальное русское».
            if z_corr - z_token < 1.0:
                return token

        return corrected_raw

    def _tokens_to_corrected(
        self, text: str, *, use_symspell: bool
    ) -> tuple[List[str], str]:
        tokens = _tokenize(text)
        if self.filter_stopwords:
            meaningful = [t for t in tokens if t not in self.stopwords]
        else:
            meaningful = tokens
        if use_symspell:
            processed = [self._normalize_token(t) for t in meaningful]
        else:
            processed = meaningful
        corrected = " ".join(processed) if processed else text
        return processed, corrected

    def normalize(self, raw: str) -> Dict[str, object]:
        """Только typo correction.

        Раньше здесь жил cartesian-product расширения через
        `dicts.synonyms` (legacy). Источник синонимов в системе теперь
        один — WordNet (см. `/expand`). JSON-словари остаются для
        domain-boost SymSpell (защищают слова `мфу`/`tshirt`/`сканер`
        от ошибочной коррекции), но как источник расширения больше
        не используются.
        """
        raw_clean = (raw or "").strip()

        working_text = raw_clean
        use_symspell = True
        if self._sage is not None:
            sage_text = self._sage.correct(raw_clean)
            if sage_text and sage_text != raw_clean:
                working_text = sage_text
                use_symspell = False

        _, corrected = self._tokens_to_corrected(
            working_text, use_symspell=use_symspell
        )

        return {
            "raw": raw_clean,
            "corrected": corrected,
            # Поля сохранены для backward-compat клиентов /normalize.
            # Реальное расширение запроса делается через /expand (WordNet).
            "synonyms": [],
            "expanded_queries": [corrected] if corrected else [raw_clean],
        }