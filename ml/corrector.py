"""Typo correctors with a common interface.

Two implementations:
- SymSpellCorrector: O(1)-ish lookup via SymSpell's prefix-deletes index.
  Preferred when `symspellpy` is installed. Uses word frequencies to break
  ties between candidates of equal edit distance.
- LevenshteinCorrector: zero-dependency fallback that brute-forces edit distance.

Both accept either an iterable of words (uniform freq=1) or a `Dict[str, int]`
mapping word → frequency.
"""

from __future__ import annotations

from typing import Dict, Iterable, List, Mapping, Protocol, Tuple, Union

VocabInput = Union[Iterable[str], Mapping[str, int]]


def _as_freq_dict(vocab: VocabInput) -> Dict[str, int]:
    if isinstance(vocab, Mapping):
        return {str(k).lower(): int(v) for k, v in vocab.items() if k}
    return {str(w).lower(): 1 for w in vocab if w}


class Corrector(Protocol):
    """Per-token spell corrector contract used by the Normalizer."""

    name: str

    def correct(self, token: str) -> str: ...


# ---------------------------------------------------------------------------
# Levenshtein fallback (pure Python, no deps).
# ---------------------------------------------------------------------------


def _levenshtein(a: str, b: str) -> int:
    if a == b:
        return 0
    if len(a) < len(b):
        a, b = b, a
    if not b:
        return len(a)
    previous = list(range(len(b) + 1))
    for i, ca in enumerate(a, start=1):
        current = [i]
        for j, cb in enumerate(b, start=1):
            ins = previous[j] + 1
            dele = current[j - 1] + 1
            sub = previous[j - 1] + (ca != cb)
            current.append(min(ins, dele, sub))
        previous = current
    return previous[-1]


class LevenshteinCorrector:
    name = "levenshtein"

    def __init__(self, vocab: VocabInput) -> None:
        freqs = _as_freq_dict(vocab)
        # Pre-sort by frequency desc so ties prefer popular words.
        self._terms: List[Tuple[str, int]] = sorted(
            freqs.items(), key=lambda kv: -kv[1]
        )
        self._term_set = set(freqs.keys())

    def correct(self, token: str) -> str:
        if not token:
            return token
        if token in self._term_set:
            return token
        budget = 1 if len(token) <= 4 else 2
        best = token
        best_d = budget + 1
        best_f = -1
        for term, freq in self._terms:
            if abs(len(term) - len(token)) > budget:
                continue
            d = _levenshtein(token, term)
            if d < best_d or (d == best_d and freq > best_f):
                best_d = d
                best_f = freq
                best = term
                if d == 0:
                    break
        return best if best_d <= budget else token


# ---------------------------------------------------------------------------
# SymSpell-based corrector.
# ---------------------------------------------------------------------------


class SymSpellCorrector:
    """Spell corrector backed by SymSpell.

    SymSpell pre-computes all "delete variants" of each dictionary term up to
    `max_edit_distance` and stores them in a hash map, so a lookup is just a
    few hash probes instead of scanning the whole vocabulary. Frequencies are
    used to rank candidates of equal edit distance.
    """

    name = "symspell"

    def __init__(self, vocab: VocabInput, max_edit_distance: int = 2,
                 prefix_length: int = 7) -> None:
        from symspellpy import SymSpell, Verbosity  # optional dep

        self._Verbosity = Verbosity
        self._max = max_edit_distance
        self._sym = SymSpell(
            max_dictionary_edit_distance=max_edit_distance,
            prefix_length=prefix_length,
        )
        for word, freq in _as_freq_dict(vocab).items():
            self._sym.create_dictionary_entry(word, max(1, freq))

    def correct(self, token: str) -> str:
        if not token:
            return token
        # Soft budget: 1 typo for short tokens, max for longer ones.
        budget = 1 if len(token) <= 4 else self._max
        suggestions = self._sym.lookup(
            token,
            self._Verbosity.TOP,  # best candidate by distance, then by frequency
            max_edit_distance=budget,
            include_unknown=False,
            transfer_casing=False,
        )
        if not suggestions:
            return token
        best = suggestions[0]
        return best.term if best.distance <= budget else token


# ---------------------------------------------------------------------------
# Factory.
# ---------------------------------------------------------------------------


def build_corrector(vocab: VocabInput, prefer: str = "symspell") -> Tuple[Corrector, str]:
    """Build the best available corrector, falling back gracefully."""
    if prefer == "symspell":
        try:
            c = SymSpellCorrector(vocab)
            return c, c.name
        except ImportError:
            pass
    c = LevenshteinCorrector(vocab)
    return c, c.name
