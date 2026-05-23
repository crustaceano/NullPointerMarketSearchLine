"""Optional product relevance scorer based on NLI (no external API).

Берёт пару (query, product_card) → выдаёт скор релевантности в [0, 1].
Под капотом — модель NLI (Natural Language Inference): для пары
(premise=карточка товара, hypothesis=запрос) предсказывает три класса:
entailment / neutral / contradiction.

Финальный скор:
    score = 0.5 + 0.5 * (P(entailment) - P(contradiction))

Свойства:
  * entailment=1                       → 1.0  (пара точно согласована)
  * neutral=1                          → 0.5  (нет ни связи, ни противоречия)
  * contradiction=1                    → 0.0  (запрос противоречит карточке)
  * непротиворечивые пары всегда ≥ противоречивых при равных prior'ах.

Базовая модель: cointegrated/rubert-base-cased-nli-threeway (~700 MB,
RuBERT-base, обучена на русских NLI-датасетах). На CPU работает
заметно быстрее multilingual-DeBERTa, поэтому жертвуем мультиязычностью
(карточки приходят преимущественно на русском).
https://huggingface.co/cointegrated/rubert-base-cased-nli-threeway

Скрипт-эвал на размеченных парах: `python ml/eval_scorer.py`.

Включается через переменную окружения:
  SCORER_ENABLED=1
Дополнительно:
  SCORER_DEVICE=cpu     (по умолчанию) или cuda
  SCORER_MODEL_ID=cointegrated/rubert-base-cased-nli-threeway   (дефолт)
                  | <другой NLI чекпоинт с label-ами entailment/contradiction>

Если модель НЕ NLI (нет в id2label `entailment`/`contradiction`) — скорер
переключается на fallback: sigmoid/softmax по выходному логиту, как
обычный cross-encoder.
"""

from __future__ import annotations

import os
import threading
from typing import Any, Iterable, Optional


MODEL_ID_DEFAULT = "cointegrated/rubert-base-cased-nli-threeway"
_MAX_LEN_DEFAULT = 256

_load_lock = threading.Lock()
_shared_instance: Optional["RelevanceScorer"] = None


def _env_enabled() -> bool:
    return os.getenv("SCORER_ENABLED", "0").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )


def product_to_text(product: Any) -> str:
    """Произвольный JSON карточки → плоский текст `key: value` для NLI.

    Рекурсивно обходит dict/list. Бизнес-логика про конкретные поля
    сюда не лезет — модель сама разберётся по тексту. Карточки
    предполагаются на русском языке (под выбранную RuBERT-NLI).
    """
    return ". ".join(_iter_kv(product, prefix=""))


def _iter_kv(node: Any, prefix: str) -> Iterable[str]:
    if isinstance(node, dict):
        for k, v in node.items():
            sub = f"{prefix}.{k}" if prefix else str(k)
            yield from _iter_kv(v, sub)
        return
    if isinstance(node, (list, tuple)):
        for item in node:
            yield from _iter_kv(item, prefix)
        return
    s = str(node).strip()
    if not s:
        return
    yield f"{prefix}: {s}" if prefix else s


class RelevanceScorer:
    """Lazy-loaded NLI-based scorer для пары (query, product_text)."""

    name = "rubert-base-cased-nli-threeway"

    def __init__(
        self,
        model_id: str | None = None,
        device: str | None = None,
        max_length: int = _MAX_LEN_DEFAULT,
    ) -> None:
        self.model_id = model_id or os.getenv("SCORER_MODEL_ID", MODEL_ID_DEFAULT)
        self.device = device or os.getenv("SCORER_DEVICE", "cpu")
        self.max_length = max_length
        self._tokenizer = None
        self._model = None
        self._ready = False
        # NLI-режим: индексы классов в выходных логитах. None = fallback.
        self._idx_entail: Optional[int] = None
        self._idx_contradict: Optional[int] = None

    def _ensure_loaded(self) -> bool:
        if self._ready:
            return True
        with _load_lock:
            if self._ready:
                return True
            try:
                import time

                from transformers import (
                    AutoModelForSequenceClassification,
                    AutoTokenizer,
                )

                t0 = time.perf_counter()
                print(
                    f"[scorer] загружаю {self.model_id} "
                    f"(первый раз ~700 MB с huggingface)...",
                    flush=True,
                )
                self._tokenizer = AutoTokenizer.from_pretrained(self.model_id)
                self._model = AutoModelForSequenceClassification.from_pretrained(
                    self.model_id
                )
                try:
                    self._model = self._model.to(self.device)
                except Exception:
                    pass
                self._model.eval()

                self._idx_entail, self._idx_contradict = self._detect_nli_indices()
                mode = (
                    "NLI"
                    if self._idx_entail is not None
                    and self._idx_contradict is not None
                    else "cross-encoder fallback"
                )

                self._ready = True
                print(
                    f"[scorer] модель загружена за "
                    f"{time.perf_counter() - t0:.1f}s, режим: {mode}",
                    flush=True,
                )
                return True
            except Exception as exc:
                print(f"[scorer] не удалось загрузить модель: {exc}", flush=True)
                return False

    def _detect_nli_indices(self) -> tuple[Optional[int], Optional[int]]:
        """Найти индексы классов entailment / contradiction в выходных логитах.

        Использует id2label из конфига модели (HF NLI-чекпоинты его всегда
        задают). Поддерживает разные написания: ENTAILMENT, entail, и т.д.
        """
        cfg = getattr(self._model, "config", None)
        id2label = getattr(cfg, "id2label", None) if cfg is not None else None
        if not id2label:
            return None, None

        idx_entail: Optional[int] = None
        idx_contradict: Optional[int] = None
        for raw_id, raw_label in id2label.items():
            try:
                idx = int(raw_id)
            except (TypeError, ValueError):
                continue
            label = str(raw_label).strip().lower()
            if "entail" in label:
                idx_entail = idx
            elif "contradict" in label:
                idx_contradict = idx
        return idx_entail, idx_contradict

    @property
    def loaded(self) -> bool:
        return self._ready

    @property
    def is_nli(self) -> bool:
        return (
            self._idx_entail is not None and self._idx_contradict is not None
        )

    def _logits_to_scores(self, logits) -> list[float]:
        """logits → list[float] в [0, 1]. NLI или fallback по форме."""
        import torch

        if self.is_nli:
            probs = torch.softmax(logits, dim=-1)
            p_ent = probs[..., self._idx_entail]
            p_con = probs[..., self._idx_contradict]
            scores = 0.5 + 0.5 * (p_ent - p_con)
            return [float(v) for v in scores.tolist()]

        # Fallback под произвольный cross-encoder.
        if logits.shape[-1] == 1:
            return [float(v) for v in torch.sigmoid(logits.squeeze(-1)).tolist()]
        probs = torch.softmax(logits, dim=-1)
        return [float(v) for v in probs[..., -1].tolist()]

    def _tokenize_pairs(self, query: str, passages: list[str]):
        """Готовит batch к forward.

        Для NLI: premise = карточка, hypothesis = запрос — порядок
        (passage, query). Для обычного cross-encoder привычный (query, passage).
        """
        assert self._tokenizer is not None
        n = len(passages)
        queries = [query] * n
        if self.is_nli:
            text_a, text_b = passages, queries
        else:
            text_a, text_b = queries, passages
        return self._tokenizer(
            text_a,
            text_b,
            padding=True,
            truncation=True,
            max_length=self.max_length,
            return_tensors="pt",
        )

    def score(self, query: str, passage: str) -> Optional[float]:
        """Скор релевантности пары (query, passage) ∈ [0, 1].

        Если passage пустой — возвращает 0. Если модель не загрузилась —
        возвращает None (вызывающий должен это обработать).
        """
        result = self.score_many(query, [passage])
        if result is None:
            return None
        return result[0]

    def score_many(
        self, query: str, passages: list[str]
    ) -> Optional[list[float]]:
        """Батчевый скоринг — N карточек против одного запроса.

        Пустые passage получают 0.0 (модель не вызывается на них).
        """
        q = (query or "").strip()
        if not q or not passages:
            return [0.0] * len(passages)
        if not self._ensure_loaded():
            return None

        keep_idx: list[int] = []
        keep_passages: list[str] = []
        for i, raw in enumerate(passages):
            p = (raw or "").strip()
            if p:
                keep_idx.append(i)
                keep_passages.append(p)

        results: list[float] = [0.0] * len(passages)
        if not keep_passages:
            return results

        try:
            import torch

            assert self._tokenizer is not None
            assert self._model is not None

            inputs = self._tokenize_pairs(q, keep_passages)
            try:
                inputs = {k: v.to(self.device) for k, v in inputs.items()}
            except Exception:
                pass

            with torch.inference_mode():
                logits = self._model(**inputs).logits

            values = self._logits_to_scores(logits)
            for idx, v in zip(keep_idx, values):
                results[idx] = v
            return results
        except Exception as exc:
            print(f"[scorer] batch inference failed: {exc}", flush=True)
            return None

    def score_pairs(
        self, premises: list[str], hypotheses: list[str]
    ) -> Optional[list[float]]:
        """Произвольные NLI-пары: для i-й пары (premises[i], hypotheses[i]).

        В отличие от `score_many`, у каждой пары своя premise. Используется,
        например, в `/expand` для фильтрации кандидатов: premise=исходный
        запрос, hypothesis=expanded query → score близок к 1, если расширение
        не противоречит оригиналу, и к 0 — если противоречит.

        Семантика scores та же, что у `score`/`score_many` (NLI или fallback).
        """
        if len(premises) != len(hypotheses):
            raise ValueError("premises and hypotheses must have the same length")
        if not premises:
            return []
        if not self._ensure_loaded():
            return None

        keep_idx: list[int] = []
        keep_a: list[str] = []
        keep_b: list[str] = []
        for i, (a_raw, b_raw) in enumerate(zip(premises, hypotheses)):
            a = (a_raw or "").strip()
            b = (b_raw or "").strip()
            if a and b:
                keep_idx.append(i)
                keep_a.append(a)
                keep_b.append(b)

        results: list[float] = [0.0] * len(premises)
        if not keep_idx:
            return results

        try:
            import torch

            assert self._tokenizer is not None
            assert self._model is not None

            # NLI-режим: text_a=premise, text_b=hypothesis. Fallback (не NLI):
            # text_a=hypothesis, text_b=premise — это условно "запрос-passage",
            # как делает обычный cross-encoder.
            if self.is_nli:
                text_a, text_b = keep_a, keep_b
            else:
                text_a, text_b = keep_b, keep_a

            inputs = self._tokenizer(
                text_a,
                text_b,
                padding=True,
                truncation=True,
                max_length=self.max_length,
                return_tensors="pt",
            )
            try:
                inputs = {k: v.to(self.device) for k, v in inputs.items()}
            except Exception:
                pass

            with torch.inference_mode():
                logits = self._model(**inputs).logits

            values = self._logits_to_scores(logits)
            for idx, v in zip(keep_idx, values):
                results[idx] = v
            return results
        except Exception as exc:
            print(f"[scorer] pairs inference failed: {exc}", flush=True)
            return None


def get_shared_scorer() -> Optional[RelevanceScorer]:
    """Singleton: одна модель в памяти на процесс FastAPI."""
    global _shared_instance
    if not _env_enabled():
        return None
    if _shared_instance is None:
        _shared_instance = RelevanceScorer()
    return _shared_instance


if __name__ == "__main__":
    # Простой бенч + sanity-check NLI: `python ml/scorer.py`
    import time

    QUERY = "летние шины 225/45 r17 michelin"
    # Намеренно разные схемы карточек — должно работать без хардкода.
    PRODUCTS_JSON: list[dict[str, Any]] = [
        # ↓ согласованная: вложенные attributes на русском
        {
            "title": "Michelin Pilot Sport 4 225/45 R17 91W",
            "brand": "Michelin",
            "attributes": {"сезон": "летние", "размер": "225/45 R17"},
        },
        # ↓ согласованная: всё на английском
        {
            "name": "Summer tyres Michelin",
            "specs": [
                {"name": "season", "value": "summer"},
                {"name": "size", "value": "225/45 R17"},
            ],
        },
        # ↓ противоречит: зимние шипы
        {
            "title": "Nokian Hakkapeliitta 9",
            "category": "Шины",
            "characteristics": {"сезон": "зимние", "шипы": True},
        },
        # ↓ противоречит: липучка зимняя
        {
            "title": "Bridgestone Blizzak",
            "params": {"season": "winter", "studded": False},
        },
        # ↓ из другой категории
        {"title": "Brother HL-L2375DWR", "category": "Лазерный принтер"},
        {"title": "Columbia мужской пуховик", "color": "чёрный"},
    ]

    sc = RelevanceScorer()
    t0 = time.perf_counter()
    sc._ensure_loaded()  # noqa: SLF001
    print(f"load: {time.perf_counter() - t0:.1f}s")
    print(f"nli mode: {sc.is_nli}\n")

    PRODUCTS = [product_to_text(p) for p in PRODUCTS_JSON]

    sc.score_many(QUERY, PRODUCTS)  # warmup
    n = 5
    t0 = time.perf_counter()
    for _ in range(n):
        scores = sc.score_many(QUERY, PRODUCTS)
    elapsed = (time.perf_counter() - t0) / n
    pairs = len(PRODUCTS)

    print(f"query: {QUERY!r}")
    print(
        f"batch={pairs}  avg={elapsed * 1000:.1f} ms  "
        f"per-pair={elapsed / pairs * 1000:.1f} ms  ≈{pairs / elapsed:.1f} pairs/s\n"
    )
    print("scores:")
    for s, p in zip(scores or [], PRODUCTS):
        print(f"  {s:.3f}  {p[:80]}")
