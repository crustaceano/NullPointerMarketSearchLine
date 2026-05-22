"""Russian morphology / lemmatization with graceful fallback.

Uses pymorphy3 when available (local, offline, no API).
Falls back to a no-op lemmatizer if the package isn't installed.

Tricks to keep results sane:
- If a parse's normal_form is present in the project vocabulary, prefer it
  (so "хлопка" → "хлопок" wins over rare homonyms).
- Otherwise prefer non-verb parses (so "шипованные" → "шипованный", not "шиповать").
"""

from __future__ import annotations

from typing import Dict, Protocol, Set


VERBY_TAGS = ("VERB", "INFN", "GRND")


class Lemmatizer(Protocol):
    name: str

    def lemma(self, token: str) -> str: ...

    def set_vocab(self, vocab: Set[str]) -> None: ...


class IdentityLemmatizer:
    """No-op lemmatizer used when pymorphy3 isn't installed."""

    name = "identity"

    def lemma(self, token: str) -> str:
        return token

    def set_vocab(self, vocab: Set[str]) -> None:
        return


class PyMorphyLemmatizer:
    """pymorphy3 wrapper with vocab-aware parse selection and a small cache."""

    name = "pymorphy3"

    def __init__(self) -> None:
        import pymorphy3  # local import keeps the dep optional

        self._morph = pymorphy3.MorphAnalyzer()
        self._cache: Dict[str, str] = {}
        self._vocab: Set[str] = set()

    def set_vocab(self, vocab: Set[str]) -> None:
        self._vocab = set(vocab)
        self._cache.clear()

    def lemma(self, token: str) -> str:
        if not token:
            return token
        cached = self._cache.get(token)
        if cached is not None:
            return cached

        parses = self._morph.parse(token)
        if not parses:
            return token

        result = parses[0].normal_form
        if self._vocab:
            in_vocab = next(
                (p for p in parses if p.normal_form in self._vocab), None
            )
            if in_vocab is not None:
                result = in_vocab.normal_form
            else:
                non_verb = next(
                    (p for p in parses if not any(t in p.tag for t in VERBY_TAGS)),
                    None,
                )
                if non_verb is not None:
                    result = non_verb.normal_form
        else:
            non_verb = next(
                (p for p in parses if not any(t in p.tag for t in VERBY_TAGS)),
                None,
            )
            if non_verb is not None:
                result = non_verb.normal_form

        if len(self._cache) < 50_000:
            self._cache[token] = result
        return result


def build_lemmatizer() -> Lemmatizer:
    """Return the best lemmatizer available, never raises."""
    try:
        return PyMorphyLemmatizer()
    except Exception:
        return IdentityLemmatizer()
