"""Optional product relevance scorer (cross-encoder transformer, no API).

Берёт пару (query, product_card) → выдаёт скор релевантности в [0, 1].
Cross-encoder = одна BERT-подобная модель, в которую засовывается
конкатенация запроса и текстового представления карточки. Точнее
bi-encoder + cosine, ценой того что нельзя закешировать эмбеддинги.

Базовая модель: DiTy/cross-encoder-russian-msmarco (~350 MB, distilbert,
обучена под русский ranking на MS MARCO).
https://huggingface.co/DiTy/cross-encoder-russian-msmarco

Включается через переменную окружения:
  SCORER_ENABLED=1
Дополнительно:
  SCORER_DEVICE=cpu     (по умолчанию) или cuda
  SCORER_MODEL_ID=DiTy/cross-encoder-russian-msmarco
                  | BAAI/bge-reranker-base   (мультиязычный вариант)
                  | <локальный путь>          (после fine-tune)
"""

from __future__ import annotations

import os
import threading
from typing import Any, Optional


MODEL_ID_DEFAULT = "DiTy/cross-encoder-russian-msmarco"
_MAX_LEN_DEFAULT = 512

_load_lock = threading.Lock()
_shared_instance: Optional["RelevanceScorer"] = None


def _env_enabled() -> bool:
    return os.getenv("SCORER_ENABLED", "0").strip().lower() in (
        "1",
        "true",
        "yes",
        "on",
    )


def product_to_text(product: dict[str, Any]) -> str:
    """Преобразует JSON карточки товара в плоский текст для cross-encoder.

    Терпимо относится к разным схемам — берёт любые из полей:
        title / name / product_name
        brand
        category
        description / desc
        attributes / specs / characteristics / params / specifications
        (dict {key: value} или list[{name|key, value|val}])
    """
    parts: list[str] = []

    title = (
        product.get("title")
        or product.get("name")
        or product.get("product_name")
    )
    if title:
        parts.append(str(title).strip())

    brand = product.get("brand") or product.get("manufacturer")
    if brand:
        parts.append(f"Бренд: {brand}")

    category = product.get("category") or product.get("category_name")
    if category:
        parts.append(f"Категория: {category}")

    description = (
        product.get("description")
        or product.get("desc")
        or product.get("short_description")
    )
    if description:
        parts.append(str(description).strip())

    for attrs_key in (
        "attributes",
        "specs",
        "characteristics",
        "params",
        "specifications",
        "properties",
    ):
        attrs = product.get(attrs_key)
        if isinstance(attrs, dict):
            for k, v in attrs.items():
                rendered = _render_value(v)
                if rendered:
                    parts.append(f"{k}: {rendered}")
        elif isinstance(attrs, list):
            for item in attrs:
                if isinstance(item, dict):
                    name = item.get("name") or item.get("key") or item.get("title")
                    val = item.get("value") or item.get("val")
                    rendered = _render_value(val)
                    if name and rendered:
                        parts.append(f"{name}: {rendered}")

    return ". ".join(p for p in parts if p)


def _render_value(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "да" if value else "нет"
    if isinstance(value, (list, tuple, set)):
        return ", ".join(str(x) for x in value if x not in (None, ""))
    return str(value).strip()


class RelevanceScorer:
    """Lazy-loaded cross-encoder для пары (query, product_text)."""

    name = "cross-encoder-russian-msmarco"

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
                    f"(первый раз ~350 MB с huggingface)...",
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
                self._ready = True
                print(
                    f"[scorer] модель загружена за {time.perf_counter() - t0:.1f}s",
                    flush=True,
                )
                return True
            except Exception as exc:
                print(f"[scorer] не удалось загрузить модель: {exc}", flush=True)
                return False

    @property
    def loaded(self) -> bool:
        return self._ready

    def score(self, query: str, passage: str) -> Optional[float]:
        """Скор релевантности пары (query, passage) ∈ [0, 1].

        Если passage пустой — возвращает 0. Если модель не загрузилась —
        возвращает None (вызывающий должен это обработать).
        """
        q = (query or "").strip()
        p = (passage or "").strip()
        if not q or not p:
            return 0.0
        if not self._ensure_loaded():
            return None

        try:
            import torch

            assert self._tokenizer is not None
            assert self._model is not None

            inputs = self._tokenizer(
                q,
                p,
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

            # Cross-encoder может выдавать 1 logit (sigmoid → [0,1])
            # либо 2 (softmax → берем класс "релевантно").
            if logits.shape[-1] == 1:
                value = torch.sigmoid(logits.squeeze(-1)).item()
            else:
                probs = torch.softmax(logits, dim=-1)
                value = probs[..., -1].item()
            return float(value)
        except Exception as exc:
            print(f"[scorer] inference failed: {exc}", flush=True)
            return None

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

            inputs = self._tokenizer(
                [q] * len(keep_passages),
                keep_passages,
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

            if logits.shape[-1] == 1:
                values = torch.sigmoid(logits.squeeze(-1)).tolist()
            else:
                values = torch.softmax(logits, dim=-1)[..., -1].tolist()

            if not isinstance(values, list):
                values = [values]
            for idx, v in zip(keep_idx, values):
                results[idx] = float(v)
            return results
        except Exception as exc:
            print(f"[scorer] batch inference failed: {exc}", flush=True)
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
    # Простой бенч: python ml/scorer.py
    import time

    QUERY = "летние шины 225/45 r17 michelin"
    PRODUCTS = [
        "Летние шины Michelin Pilot Sport 4 225/45 R17 91W. Бренд: Michelin",
        "Зимние шипованные Nokian Hakkapeliitta 235/55 R19 105T",
        "Continental WinterContact TS 870 195/65 R15. Бренд: Continental",
        "Принтер Brother HL-L2375DWR. Бренд: Brother. Категория: Оргтехника",
        "Куртка мужская зимняя пуховик чёрная Columbia",
    ]

    sc = RelevanceScorer()
    t0 = time.perf_counter()
    sc._ensure_loaded()  # noqa: SLF001
    print(f"load: {time.perf_counter() - t0:.1f}s\n")

    sc.score_many(QUERY, PRODUCTS)  # warmup
    n = 5
    t0 = time.perf_counter()
    for _ in range(n):
        scores = sc.score_many(QUERY, PRODUCTS)
    elapsed = (time.perf_counter() - t0) / n
    pairs = len(PRODUCTS)

    print(f"query: {QUERY!r}")
    print(f"batch: {pairs} продуктов")
    print(
        f"avg:   {elapsed * 1000:.1f} ms / батч  "
        f"({elapsed / pairs * 1000:.1f} ms / пара, "
        f"≈{pairs / elapsed:.1f} пар/с)\n"
    )
    print("scores (последний прогон):")
    for s, p in zip(scores or [], PRODUCTS):
        print(f"  {s:.3f}  {p[:70]}")
