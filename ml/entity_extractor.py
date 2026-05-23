"""Optional entity extractor using GLiNER (multilingual NER, no external API).

Model: urchade/gliner_multi-v2.1
https://huggingface.co/urchade/gliner_multi-v2.1

Включается через переменную окружения:
  GLINER_ENABLED=1
Дополнительно:
  GLINER_DEVICE=cpu     (по умолчанию) или cuda
  GLINER_MODEL_ID=urchade/gliner_multi-v2.1
  GLINER_THRESHOLD=0.5  (минимальная уверенность для попадания в JSON)
"""

from __future__ import annotations

import os
import threading
from typing import Dict, List, Optional


MODEL_ID_DEFAULT = "urchade/gliner_multi-v2.1"

# Универсальный набор лейблов для трёх тестовых категорий
# (одежда, шины, оргтехника). GLiNER принимает любой язык, но
# на русских запросах русские лейблы дают лучшие результаты.
DEFAULT_LABELS: List[str] = [
    "товар",            # футболка, шина, принтер, монитор, ноутбук
    "бренд",            # nike, michelin, brother, canon, kyocera
    "модель",           # air max 90, x-ice, imagerunner, latitude
    "цвет",             # чёрный, бежевый, красный (одежда + тонеры)
    "материал",         # хлопок, кожа, металл, пластик
    "размер",           # xs / m / 42 / 225/45 r17 / a4 / 27"
    "пол",              # мужской, женский, детский, унисекс
    "сезон",            # летний, зимний, всесезонный, демисезонный
    "стиль",            # спортивный, классический, повседневный
    "тип конструкции",  # шипованные, runflat, лазерный, струйный, ips
    "назначение",       # для офиса, для дома, для авто, для презентаций
    "формат",           # a4, a3, a0, a1 (бумага / плоттеры / мфу)
    "интерфейс",        # usb, bluetooth, wi-fi, hdmi, lightning
    "индекс нагрузки",  # 91, 98, 106 (шины)
    "индекс скорости",  # h, v, t, w, y (шины)
]

# Лейблы под конкретную категорию: меньше шума, выше точность,
# когда роутер уже знает в какую категорию попал запрос.
LABELS_BY_CATEGORY: Dict[str, List[str]] = {
    "одежда": [
        "товар",
        "бренд",
        "модель",
        "цвет",
        "материал",
        "размер",
        "пол",
        "сезон",
        "стиль",
    ],
    "шины": [
        "товар",
        "бренд",
        "модель",
        "сезон",
        "тип конструкции",
        "размер",
        "индекс нагрузки",
        "индекс скорости",
        "назначение",
    ],
    "оргтехника": [
        "товар",
        "бренд",
        "модель",
        "цвет",
        "формат",
        "интерфейс",
        "тип конструкции",
        "назначение",
    ],
}

# Маппинг для совместимости — оставлен пустым, ключ модели уже на русском.
LABEL_RU: Dict[str, str] = {}

_load_lock = threading.Lock()
_shared_instance: Optional["EntityExtractor"] = None


def _env_enabled() -> bool:
    return os.getenv("GLINER_ENABLED", "0").strip().lower() in ("1", "true", "yes", "on")


class EntityExtractor:
    """Lazy-loaded GLiNER wrapper for marketplace query entity extraction."""

    name = "gliner_multi-v2.1"

    def __init__(
        self,
        model_id: str | None = None,
        device: str | None = None,
        threshold: float | None = None,
        labels: List[str] | None = None,
    ) -> None:
        self.model_id = model_id or os.getenv("GLINER_MODEL_ID", MODEL_ID_DEFAULT)
        self.device = device or os.getenv("GLINER_DEVICE", "cpu")
        self.threshold = (
            threshold
            if threshold is not None
            else float(os.getenv("GLINER_THRESHOLD", "0.5") or "0.5")
        )
        self.labels = labels or DEFAULT_LABELS
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

                from gliner import GLiNER

                t0 = time.perf_counter()
                print(
                    f"[gliner] загружаю {self.model_id} "
                    f"(первый раз ~200 MB с huggingface)...",
                    flush=True,
                )
                self._model = GLiNER.from_pretrained(self.model_id)
                try:
                    self._model = self._model.to(self.device)
                except Exception:
                    pass
                self._ready = True
                print(
                    f"[gliner] модель загружена за {time.perf_counter() - t0:.1f}s",
                    flush=True,
                )
                return True
            except Exception as exc:
                print(f"[gliner] не удалось загрузить модель: {exc}", flush=True)
                return False

    @property
    def loaded(self) -> bool:
        return self._ready

    def extract(
        self,
        text: str,
        category: Optional[str] = None,
    ) -> Dict[str, List[str]]:
        """Вернуть JSON-словарь {label_ru: [значения]} для одного запроса.

        Если передана `category` (`одежда` / `шины` / `оргтехника`) —
        используется узкий набор лейблов под эту категорию. Иначе —
        универсальный `self.labels`.
        """
        cleaned = (text or "").strip()
        if not cleaned or not self._ensure_loaded():
            return {}

        labels = self.labels
        if category:
            cat_labels = LABELS_BY_CATEGORY.get(category.strip().lower())
            if cat_labels:
                labels = cat_labels

        try:
            assert self._model is not None
            raw = self._model.predict_entities(
                cleaned,
                labels,
                threshold=self.threshold,
            )
        except Exception as exc:
            print(f"[gliner] ошибка inference: {exc}")
            return {}

        out: Dict[str, List[str]] = {}
        for ent in raw:
            label = LABEL_RU.get(ent["label"], ent["label"])
            value = ent["text"].strip().lower()
            if not value:
                continue
            bucket = out.setdefault(label, [])
            if value not in bucket:
                bucket.append(value)
        return out


def get_shared_entity_extractor() -> Optional[EntityExtractor]:
    """Singleton: одна модель в памяти на процесс FastAPI."""
    global _shared_instance
    if not _env_enabled():
        return None
    if _shared_instance is None:
        _shared_instance = EntityExtractor()
    return _shared_instance
