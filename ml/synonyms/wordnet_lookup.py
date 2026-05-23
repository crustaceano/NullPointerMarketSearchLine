"""Тонкая обёртка над `ruwordnet` — единственный источник синонимов в pipeline.

Дизайн:
  * Lazy-load: SQLite-БД RuWordNet поднимается только при первом lookup.
  * Кеш: результаты `lookup(word)` мемоизируются в LRU.
  * Без эвристик про конкретные слова. Шумные кандидаты (полисемичные,
    из чужого домена, контекстуально невалидные) отсекаются NLI-фильтром
    в роутере /expand — там трансформер видит весь запрос и кандидата
    целиком, поэтому решает точнее любых wordlist-эвристик.

Зачем не хранить «свою» БД:
  * Маркетплейс-агностичность: RuWordNet общерусский, не привязан к одной
    категории.
  * Масштабируемость: ручной concept-DB не растёт самостоятельно, а
    тезаурус один раз скачали — работает на всём вокабуляре.
  * Trade-off: WordNet не знает доменный жаргон (`зипка`, `мфу`,
    `липучка-как-шины`). Это осознанное ограничение.
"""

from __future__ import annotations

import threading
from functools import lru_cache
from typing import Optional


_lock = threading.Lock()
_shared: Optional["WordNetLookup"] = None


class WordNetLookup:
    """Lazy-loaded source of synonyms (RuWordNet)."""

    name = "ruwordnet"

    def __init__(self) -> None:
        self._wn = None
        self._ready = False
        # ВАЖНО: lru_cache на bound-методе течёт «глобально» — здесь это
        # ровно то, что нам нужно (один экземпляр per-process).
        self._cached: "Callable[[str], tuple[str, ...]]" = lru_cache(maxsize=8192)(  # type: ignore[name-defined]
            self._lookup_uncached
        )

    def _ensure_loaded(self) -> bool:
        if self._ready:
            return True
        with _lock:
            if self._ready:
                return True
            try:
                from ruwordnet import RuWordNet  # type: ignore

                self._wn = RuWordNet()
                self._ready = True
                return True
            except Exception as exc:
                print(f"[wordnet] не удалось загрузить ruwordnet: {exc}", flush=True)
                return False

    @property
    def loaded(self) -> bool:
        return self._ready

    def lookup(self, word: str) -> list[str]:
        """Список синонимов слова (без него самого, lower-cased, отсортирован).

        Пустая строка / неизвестное слово / провал загрузки → `[]`.
        Кеш в памяти процесса; повторные запросы — O(1).
        """
        w = (word or "").strip().lower()
        if not w:
            return []
        if not self._ensure_loaded():
            return []
        return list(self._cached(w))

    def _lookup_uncached(self, word: str) -> tuple[str, ...]:
        assert self._wn is not None
        out: set[str] = set()
        try:
            for ss in self._wn.get_synsets(word):
                for sense in ss.senses:
                    name = sense.name.lower().strip()
                    if name and name != word:
                        out.add(name)
        except Exception as exc:
            print(f"[wordnet] lookup({word!r}) failed: {exc}", flush=True)
            return ()
        return tuple(sorted(out))


def get_shared_wordnet() -> WordNetLookup:
    """Singleton — одна загрузка RuWordNet на процесс."""
    global _shared
    if _shared is None:
        _shared = WordNetLookup()
    return _shared
