"""Vocabulary and stopword builders.

SymSpell dictionary (Russian):
- Primary: ml/data/ru-100k.txt  (word + frequency per line, OpenSubtitles-style)
- Fallback: wordfreq top-N if the file is missing

Domain boosts still come from ml/dictionaries/*.json
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Dict, Iterable, Optional, Set


def stopwords_filter_enabled() -> bool:
    """Filter stopwords only when STOPWORDS_FILTER=1 (off by default)."""
    return os.getenv("STOPWORDS_FILTER", "0").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )


DATA_DIR = Path(__file__).resolve().parent / "data"
DEFAULT_FREQ_FILE = DATA_DIR / "ru-100k.txt"

DOMAIN_TERM_BOOST = 12_000
DOMAIN_SYNONYM_BOOST = 9_000
ZIPF_SCALE = 1_000


def load_frequency_file(
    path: Path,
    max_words: Optional[int] = None,
) -> Dict[str, int]:
    """Load SymSpell-style frequency list: `<word> <count>` per line."""
    vocab: Dict[str, int] = {}
    if not path.exists():
        return vocab

    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            parts = line.split()
            if len(parts) < 2:
                continue
            word = parts[0].lower()
            try:
                freq = int(parts[1])
            except ValueError:
                continue
            if not word:
                continue
            vocab[word] = max(vocab.get(word, 0), freq)
            if max_words is not None and len(vocab) >= max_words:
                break
    return vocab


def build_general_vocab(
    top_n: Optional[int] = 100_000,
    freq_path: Optional[Path] = None,
) -> Dict[str, int]:
    """Build Russian word frequencies for SymSpell."""
    path = freq_path or DEFAULT_FREQ_FILE
    if path.exists():
        loaded = load_frequency_file(path, max_words=top_n)
        if loaded:
            return loaded

    # Fallback when ru-100k.txt is absent.
    vocab: Dict[str, int] = {}
    try:
        from wordfreq import top_n_list, zipf_frequency
    except ImportError:
        return vocab

    limit = top_n or 30_000
    for word in top_n_list("ru", limit):
        zipf = zipf_frequency(word, "ru")
        if zipf <= 0:
            continue
        vocab[word.lower()] = max(1, int(zipf * ZIPF_SCALE))
    return vocab


def merge_domain(
    base: Dict[str, int],
    domain_terms: Iterable[str],
    domain_synonyms: Iterable[str],
) -> Dict[str, int]:
    """Merge domain words into the base vocab with boosted frequencies."""
    for term in domain_synonyms:
        t = term.lower().strip()
        if t:
            base[t] = max(base.get(t, 0), DOMAIN_SYNONYM_BOOST)
    for term in domain_terms:
        t = term.lower().strip()
        if t:
            base[t] = max(base.get(t, 0), DOMAIN_TERM_BOOST)
    return base


def build_stopwords(extra: Iterable[str] = ()) -> Set[str]:
    """Russian stopwords from `stop-words` (filtering is controlled separately)."""
    stops: Set[str] = set()
    try:
        from stop_words import get_stop_words
        stops.update(w.lower() for w in get_stop_words("russian"))
    except Exception:
        stops.update(
            ["и", "в", "на", "с", "из", "для", "по", "к", "о",
             "за", "от", "у", "не", "а", "но", "или"]
        )
    stops.update(["шт", "штук", "пара", "комплект", "набор"])
    for w in extra:
        if w:
            stops.add(w.lower())
    return stops
