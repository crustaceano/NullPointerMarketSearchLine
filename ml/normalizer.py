"""Lightweight query normalizer.

No external ML libraries; just JSON dictionaries and a Levenshtein distance.
Designed so we can later swap in a real model without changing callers.
"""

from __future__ import annotations

import itertools
import json
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, Iterable, List, Set, Tuple


TOKEN_RE = re.compile(r"[\w\-]+", flags=re.UNICODE)


@dataclass
class Dictionaries:
    """Aggregated terms and synonyms loaded from JSON files."""

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


def levenshtein(a: str, b: str) -> int:
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


def _tokenize(text: str) -> List[str]:
    return [m.group(0).lower() for m in TOKEN_RE.finditer(text)]


def _correct_token(token: str, terms: Iterable[str]) -> Tuple[str, bool]:
    """Return (best_token, corrected?) using Levenshtein with a small budget."""
    if token in terms:
        return token, False
    # Allow at most 1 typo for short tokens, 2 for longer ones.
    budget = 1 if len(token) <= 4 else 2
    best = token
    best_d = budget + 1
    for term in terms:
        if abs(len(term) - len(token)) > budget:
            continue
        d = levenshtein(token, term)
        if d < best_d:
            best_d = d
            best = term
            if d == 0:
                break
    return (best, best != token) if best_d <= budget else (token, False)


class Normalizer:
    """Stateless query normalizer over the loaded dictionaries."""

    def __init__(self, dicts: Dictionaries, max_expansions: int = 6) -> None:
        self.dicts = dicts
        self.max_expansions = max_expansions

    def normalize(self, raw: str) -> Dict[str, object]:
        raw_clean = (raw or "").strip()
        tokens = _tokenize(raw_clean)

        corrected_tokens: List[str] = []
        for tok in tokens:
            best, _ = _correct_token(tok, self.dicts.terms)
            corrected_tokens.append(best)

        corrected = " ".join(corrected_tokens) if corrected_tokens else raw_clean

        # Synonyms: collect alternatives per token, keep the first 3 unique.
        synonyms: List[str] = []
        per_token_alts: List[List[str]] = []
        for tok in corrected_tokens:
            alts = [tok] + [s for s in self.dicts.synonyms.get(tok, []) if s != tok]
            per_token_alts.append(alts)
            for s in alts[1:]:
                if s not in synonyms:
                    synonyms.append(s)

        # Expanded queries: cartesian product across token alternatives, capped.
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
