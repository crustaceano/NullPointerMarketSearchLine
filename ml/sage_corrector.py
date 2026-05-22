"""Optional full-sentence spell checker using SAGE FRED-T5 (local, no API).

Model: ai-forever/sage-fredt5-distilled-95m (~95M params, ~380 MB)
https://huggingface.co/ai-forever/sage-fredt5-distilled-95m

Enable with environment variable:
  SAGE_ENABLED=1
Optional:
  SAGE_DEVICE=cpu   (default) or cuda
  SAGE_MODEL_ID=ai-forever/sage-fredt5-distilled-95m
"""

from __future__ import annotations

import os
import threading
from typing import Optional


MODEL_ID_DEFAULT = "ai-forever/sage-fredt5-distilled-95m"
_load_lock = threading.Lock()
_shared_instance: Optional["SageCorrector"] = None


def _env_enabled() -> bool:
    return os.getenv("SAGE_ENABLED", "0").strip().lower() in ("1", "true", "yes", "on")


class SageCorrector:
    """Lazy-loaded seq2seq spell checker for Russian queries."""

    name = "sage-fredt5-distilled-95m"

    def __init__(self, model_id: str | None = None, device: str | None = None) -> None:
        self.model_id = model_id or os.getenv("SAGE_MODEL_ID", MODEL_ID_DEFAULT)
        self.device = device or os.getenv("SAGE_DEVICE", "cpu")
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
                import torch
                from transformers import AutoModelForSeq2SeqLM, AutoTokenizer

                self._tokenizer = AutoTokenizer.from_pretrained(self.model_id)
                self._model = AutoModelForSeq2SeqLM.from_pretrained(self.model_id)
                self._model.to(self.device)
                self._model.eval()
                self._ready = True
                return True
            except Exception as exc:
                print(f"[sage] failed to load model: {exc}")
                return False

    @property
    def loaded(self) -> bool:
        return self._ready

    def correct(self, text: str) -> str:
        """Return spell-corrected text, or original on failure / empty input."""
        cleaned = (text or "").strip()
        if not cleaned:
            return text or ""

        if not self._ensure_loaded():
            return cleaned

        try:
            import torch

            assert self._tokenizer is not None
            assert self._model is not None

            inputs = self._tokenizer(
                cleaned,
                max_length=None,
                padding="longest",
                truncation=True,
                return_tensors="pt",
            )
            input_ids = inputs["input_ids"]
            max_new = max(8, int(input_ids.size(1) * 1.5))

            with torch.no_grad():
                outputs = self._model.generate(
                    **{k: v.to(self.device) for k, v in inputs.items()},
                    max_new_tokens=max_new,
                )

            result = self._tokenizer.batch_decode(outputs, skip_special_tokens=True)[0]
            return (result or cleaned).strip()
        except Exception as exc:
            print(f"[sage] inference failed: {exc}")
            return cleaned


def build_sage_corrector() -> Optional[SageCorrector]:
    """Return SageCorrector only when SAGE_ENABLED=1."""
    if not _env_enabled():
        return None
    return SageCorrector()


def get_shared_sage_corrector() -> Optional[SageCorrector]:
    """Singleton for FastAPI process (one model in memory)."""
    global _shared_instance
    if not _env_enabled():
        return None
    if _shared_instance is None:
        _shared_instance = SageCorrector()
    return _shared_instance
