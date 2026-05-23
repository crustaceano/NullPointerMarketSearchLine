"""Pseudo-perplexity фильтр для expanded queries.

Идея: после WordNet-замены получаются варианты вроде «баллон зимняя с шипами».
По отдельности слова валидны, но цепочка грамматически/семантически кривая.
NLI такое плохо ловит (нет противоречия с оригиналом), а LM — отлично:
у бредовой комбинации высокая perplexity.

Используем pseudo-perplexity на маленьком MLM (distilrubert-tiny, ~50 MB):
для каждой позиции маскируем токен и считаем -log P(original | context).
Один forward на фразу за счёт батч-маскирования.

Решение принимаем по **относительному** PPL: candidate / original. Абсолютный
threshold плох — зависит от длины и редкости слов; relative робастнее.

Префикс-контекст (`LM_FILTER_PREFIX`, дефолт `"купить "`):
  Маркетплейс-запросы LM «видела» в форме `купить X` гораздо чаще, чем
  голое `X`. Префикс прокидывается в input как контекст, но НЕ маскируется
  и НЕ учитывается в loss — оцениваем именно фразу-кандидат, на разумной
  условной вероятности `P(кандидат | "купить ")`. Пустой `LM_FILTER_PREFIX=""`
  отключает фичу.

Фильтр включается через ENV; если модель не загрузилась — фильтр выключается
тихо (None из get_shared_lm_filter), expand работает как раньше.

ENV:
  LM_FILTER_ENABLED=1            (default)
  LM_FILTER_MODEL_ID=DeepPavlov/distilrubert-tiny-cased-conversational
  LM_FILTER_PPL_RATIO=2.0        # candidate выкидывается, если ppl > original * ratio
  LM_FILTER_PREFIX="купить "     # context-префикс перед фразой при PPL
  LM_FILTER_DEVICE=cpu
"""

from __future__ import annotations

import math
import os
import threading
from typing import Optional


MODEL_ID_DEFAULT = "DeepPavlov/distilrubert-tiny-cased-conversational"
_MAX_LEN_DEFAULT = 64

_load_lock = threading.Lock()
_shared_instance: Optional["LMFilter"] = None


def _env_bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def _env_float(name: str, default: float) -> float:
    raw = os.getenv(name)
    if raw is None:
        return default
    try:
        return float(raw)
    except ValueError:
        return default


class LMFilter:
    """Lazy-loaded MLM для pseudo-perplexity коротких фраз."""

    name = "distilrubert-tiny-pll"

    def __init__(
        self,
        model_id: str | None = None,
        device: str | None = None,
        max_length: int = _MAX_LEN_DEFAULT,
        prefix: str | None = None,
    ) -> None:
        self.model_id = model_id or os.getenv("LM_FILTER_MODEL_ID", MODEL_ID_DEFAULT)
        self.device = device or os.getenv("LM_FILTER_DEVICE", "cpu")
        self.max_length = max_length
        # Контекстный префикс для PPL: маркетплейс-запросы естественнее
        # звучат с "купить ..." впереди. ENV пуст → выключено.
        self.prefix = prefix if prefix is not None else os.getenv(
            "LM_FILTER_PREFIX", "купить "
        )
        self._tokenizer = None
        self._model = None
        self._mask_id: Optional[int] = None
        self._prefix_len: int = 0  # сколько subword-токенов занимает префикс
        self._ready = False

    def _ensure_loaded(self) -> bool:
        if self._ready:
            return True
        with _load_lock:
            if self._ready:
                return True
            try:
                import time

                from transformers import AutoModelForMaskedLM, AutoTokenizer

                t0 = time.perf_counter()
                print(
                    f"[lm-filter] загружаю {self.model_id} (~50 MB)...",
                    flush=True,
                )
                self._tokenizer = AutoTokenizer.from_pretrained(self.model_id)
                self._model = AutoModelForMaskedLM.from_pretrained(self.model_id)
                try:
                    self._model = self._model.to(self.device)
                except Exception:
                    pass
                self._model.eval()

                self._mask_id = self._tokenizer.mask_token_id
                if self._mask_id is None:
                    print(
                        "[lm-filter] у токенизатора нет mask_token — "
                        "MLM-фильтр невозможен",
                        flush=True,
                    )
                    return False

                # Длина префикса в subword-токенах. Считаем без спец-токенов:
                # в полном input их добавит сам токенизатор, и мы их пропускаем
                # через special_tokens_mask. Префикс уйдёт сразу после [CLS],
                # так что _prefix_len — это число первых non-special позиций,
                # которые мы НЕ маскируем (контекст).
                if self.prefix:
                    pre_ids = self._tokenizer(
                        self.prefix, add_special_tokens=False
                    )["input_ids"]
                    self._prefix_len = len(pre_ids)
                else:
                    self._prefix_len = 0

                self._ready = True
                print(
                    f"[lm-filter] готов за {time.perf_counter() - t0:.1f}s; "
                    f"prefix={self.prefix!r} ({self._prefix_len} tok)",
                    flush=True,
                )
                return True
            except Exception as exc:
                print(f"[lm-filter] не удалось загрузить: {exc}", flush=True)
                return False

    @property
    def loaded(self) -> bool:
        return self._ready

    def perplexity(self, text: str) -> Optional[float]:
        """Pseudo-perplexity одной фразы. None если не получилось.

        Один forward на фразу: батчуем по числу маскируемых позиций.
        """
        results = self.perplexity_many([text])
        if results is None:
            return None
        return results[0]

    def perplexity_many(self, texts: list[str]) -> Optional[list[float]]:
        """Pseudo-PPL для списка фраз. Возвращает list той же длины (None
        для пустых) или None если модель не загрузилась."""
        if not texts:
            return []
        if not self._ensure_loaded():
            return None
        out: list[float] = []
        for t in texts:
            t = (t or "").strip()
            if not t:
                out.append(float("inf"))
                continue
            try:
                ppl = self._pll_one(t)
            except Exception as exc:
                print(f"[lm-filter] PPL({t!r}) failed: {exc}", flush=True)
                ppl = float("inf")
            out.append(ppl)
        return out

    def _pll_one(self, text: str) -> float:
        import torch

        assert self._tokenizer is not None
        assert self._model is not None
        assert self._mask_id is not None

        # Префикс прокидываем как часть input — он останется в видимости
        # модели, но в loss НЕ войдёт (см. срез positions ниже).
        full_text = (self.prefix + text) if self.prefix else text

        enc = self._tokenizer(
            full_text,
            truncation=True,
            max_length=self.max_length,
            return_tensors="pt",
            return_special_tokens_mask=True,
        )
        input_ids = enc["input_ids"][0]  # (L,)
        special = enc["special_tokens_mask"][0].bool()  # (L,)
        all_positions = (~special).nonzero(as_tuple=True)[0]  # non-special tokens
        # Первые prefix_len non-special — это сам префикс ("купить"). Их
        # не маскируем и не учитываем — это контекст, а не оцениваемая часть.
        if self._prefix_len and all_positions.shape[0] > self._prefix_len:
            positions = all_positions[self._prefix_len:]
        else:
            positions = all_positions
        n = positions.shape[0]
        if n == 0:
            return float("inf")

        # Батч размера n: на i-й строке маскирована позиция positions[i].
        batch = input_ids.unsqueeze(0).repeat(n, 1).clone()
        for row, pos in enumerate(positions.tolist()):
            batch[row, pos] = self._mask_id
        attention = torch.ones_like(batch)

        try:
            batch = batch.to(self.device)
            attention = attention.to(self.device)
        except Exception:
            pass

        with torch.inference_mode():
            logits = self._model(input_ids=batch, attention_mask=attention).logits
        # Для каждой строки достаём log-prob исходного токена в его позиции.
        log_probs = torch.log_softmax(logits, dim=-1)
        total = 0.0
        for row, pos in enumerate(positions.tolist()):
            target = int(input_ids[pos])
            total -= float(log_probs[row, pos, target])
        return math.exp(total / n)


def get_shared_lm_filter() -> Optional[LMFilter]:
    """Singleton. None — если фильтр выключен флагом."""
    global _shared_instance
    if not _env_bool("LM_FILTER_ENABLED", default=True):
        return None
    if _shared_instance is None:
        _shared_instance = LMFilter()
    return _shared_instance


def lm_filter_ratio() -> float:
    """Кандидат отбрасывается, если ppl(cand) > ppl(orig) * ratio.

    Дефолт 2.0 — мягкий: режет только заметно более «странные» фразы.
    1.5 — агрессивнее. 3.0+ — почти ничего не режет, остаётся как
    safety-net против явного бреда.
    """
    return _env_float("LM_FILTER_PPL_RATIO", 2.0)


if __name__ == "__main__":
    # Sanity-check: ожидаем что бредовые фразы получают высокий PPL.
    f = LMFilter()
    if not f._ensure_loaded():  # noqa: SLF001
        raise SystemExit("model not loaded")

    cases = [
        "летние шины 225/45 r17",
        "летние покрышки 225/45 r17",
        "летние баллон 225/45 r17",
        "баллон зимняя с шипами",
        "толстовка с капюшоном",
        "куртка с капюшоном",
        "зипка чёрная xl",
    ]
    ppls = f.perplexity_many(cases) or []
    base = ppls[0] if ppls else 1.0
    for text, p in zip(cases, ppls):
        flag = "OK" if p <= base * 2.0 else "DROP"
        print(f"  [{flag}] ppl={p:8.2f}  ratio={p / base:5.2f}  {text}")
