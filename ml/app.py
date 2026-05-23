"""FastAPI entrypoint for the lightweight ML service.

Сами эндпоинты живут в `routers/` — этот файл только собирает их в
единое FastAPI-приложение. Импорт `routers.deps` инициализирует
синглтон Normalizer один раз на процесс (см. `routers/deps.py`).
"""

from __future__ import annotations

from fastapi import FastAPI

from routers import expand, extract, health, normalize, score


app = FastAPI(title="NullPointer ML Normalizer", version="0.10.0")

app.include_router(health.router)
app.include_router(normalize.router)
app.include_router(extract.router)
app.include_router(score.router)
app.include_router(expand.router)
