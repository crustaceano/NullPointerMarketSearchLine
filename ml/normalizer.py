"""Lightweight query normalizer.

Pipeline:
  raw → [optional SAGE full-sentence cleanup] →
  tokenize → [drop stopwords only if STOPWORDS_FILTER=1] → [SymSpell] →
  collect synonyms → build expanded queries.

When SAGE_ENABLED=1, SymSpell per-token step is skipped (SAGE already fixed text).

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

import itertools
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

    def _normalize_token(self, token: str) -> str:
        """Resolve a token to its best searchable form."""
        # Keep sizes/articles/models unchanged: 15, 42, x5, 4060ti.
        if not any(ch.isalpha() for ch in token):
            return token

        # Conservative mode: do not "fix" already valid words.
        # 1) domain word
        # 2) known by built vocabulary
        # 3) common Russian word by zipf_frequency
        if token in self._domain_vocab or token in self._full_vocab:
            return token
        if self._zipf_frequency is not None and self._zipf_frequency(token, "ru") >= 2.0:
            return token

        corrected_raw = self.corrector.correct(token)
        if corrected_raw != token and corrected_raw in self._domain_vocab:
            return corrected_raw

        # Temporarily disable lemmatization/infinitive conversion:
        # keep corrected surface form exactly as user sees it.
        if corrected_raw != token:
            return corrected_raw

        return token

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
        raw_clean = (raw or "").strip()

        working_text = raw_clean
        use_symspell = True
        if self._sage is not None:
            sage_text = self._sage.correct(raw_clean)
            if sage_text and sage_text != raw_clean:
                working_text = sage_text
                use_symspell = False

        processed, corrected = self._tokens_to_corrected(
            working_text, use_symspell=use_symspell
        )

        synonyms: List[str] = []
        per_token_alts: List[List[str]] = []
        for tok in processed:
            alts = [tok] + [s for s in self.dicts.synonyms.get(tok, []) if s != tok]
            per_token_alts.append(alts)
            for s in alts[1:]:
                if s not in synonyms:
                    synonyms.append(s)

        expanded: List[str] = []
        if per_token_alts:
            for combo in itertools.product(*per_token_alts):
                q = " ".join(combo)
                if q and q not in expanded:
                    expanded.append(q)
                if len(expanded) >= self.max_expansions:
                    break
        if corrected and corrected not in expanded:
            expanded.insert(0, corrected)
        if not expanded:
            expanded = [raw_clean]

        return {
            "raw": raw_clean,
            "corrected": corrected,
            "synonyms": synonyms[:10],
            "expanded_queries": expanded[: self.max_expansions],
        }