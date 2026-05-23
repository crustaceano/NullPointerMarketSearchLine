"""Eval RelevanceScorer на размеченных парах (query, product).

Источник: data/scorer/golden.jsonl. Каждая строка:
    {"group": "...", "query": "...", "product": "...",
     "label": "consistent" | "contradicts"}

Группа объединяет один query с несколькими карточками (consistent +
contradicts). Так удобно мерить что внутри группы scorer ставит
непротиворечивые пары выше противоречивых.

Метрики:
  * mean_consistent / mean_contradicts — средний скор по классам.
  * pairwise_accuracy — для каждой пары (consistent, contradicts) внутри
    одной группы: % случаев когда score(consistent) > score(contradicts).
    Это и есть "четкость сортировки" — главное свойство ранжера.
  * roc_auc — глобальная AUC (consistent=1, contradicts=0).

Запуск:
    python ml/eval_scorer.py
    python ml/eval_scorer.py --golden data/scorer/golden.jsonl --threshold 0.5
"""

from __future__ import annotations

import argparse
import json
import os
import statistics
from collections import defaultdict
from pathlib import Path
from typing import Iterable

# scorer.py имеет lazy load + env-флаг — выставим, чтобы не парился.
os.environ.setdefault("SCORER_ENABLED", "1")

from scorer import RelevanceScorer  # noqa: E402

ROOT = Path(__file__).resolve().parent
DEFAULT_GOLDEN = ROOT / "data" / "scorer" / "golden.jsonl"

LABEL_CONSISTENT = "consistent"
LABEL_CONTRADICTS = "contradicts"


def _load_golden(path: Path) -> list[dict]:
    rows: list[dict] = []
    with path.open(encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("//"):
                continue
            rows.append(json.loads(line))
    return rows


def _roc_auc(scores: list[float], labels: list[int]) -> float:
    """Площадь под ROC через попарные сравнения. Без sklearn."""
    pos = [s for s, y in zip(scores, labels) if y == 1]
    neg = [s for s, y in zip(scores, labels) if y == 0]
    if not pos or not neg:
        return float("nan")
    wins = ties = 0
    for s_pos in pos:
        for s_neg in neg:
            if s_pos > s_neg:
                wins += 1
            elif s_pos == s_neg:
                ties += 1
    total = len(pos) * len(neg)
    return (wins + 0.5 * ties) / total


def _pairwise_ranking(rows: Iterable[dict]) -> tuple[float, int, int]:
    """Внутри каждой группы — все пары (consistent, contradicts).

    Возвращает (accuracy, correct, total).
    """
    by_group: dict[str, list[dict]] = defaultdict(list)
    for r in rows:
        by_group[r["group"]].append(r)

    correct = 0
    total = 0
    for group_rows in by_group.values():
        cons = [r for r in group_rows if r["label"] == LABEL_CONSISTENT]
        cont = [r for r in group_rows if r["label"] == LABEL_CONTRADICTS]
        for c in cons:
            for x in cont:
                total += 1
                if c["_score"] > x["_score"]:
                    correct += 1
                elif c["_score"] == x["_score"]:
                    correct += 0.5  # tie → засчитываем половину
    if total == 0:
        return float("nan"), 0, 0
    return correct / total, correct, total


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--golden", default=str(DEFAULT_GOLDEN))
    ap.add_argument(
        "--threshold",
        type=float,
        default=0.5,
        help="Порог бинарной классификации для accuracy",
    )
    ap.add_argument(
        "--print-failures",
        action="store_true",
        help="Печатать пары где scorer ошибся (consistent < contradicts)",
    )
    args = ap.parse_args()

    rows = _load_golden(Path(args.golden))
    print(f"loaded: {len(rows)} pairs from {args.golden}")
    groups = sorted({r["group"] for r in rows})
    print(f"groups: {len(groups)} ({', '.join(groups)})\n")

    sc = RelevanceScorer()
    if not sc._ensure_loaded():  # noqa: SLF001
        print("FAILED: модель не загрузилась")
        return
    print(f"model:    {sc.model_id}")
    print(f"nli_mode: {sc.is_nli}\n")

    # score_many поддерживает только один query на батч, поэтому пробежим
    # по группам — это естественнее и совпадает с реальным /score.
    by_group: dict[str, list[int]] = defaultdict(list)
    for i, r in enumerate(rows):
        by_group[r["group"]].append(i)

    scores: list[float] = [0.0] * len(rows)
    for g, idxs in by_group.items():
        q = rows[idxs[0]]["query"]
        # Внутри группы query одинаковый
        assert all(rows[i]["query"] == q for i in idxs), f"mixed query in {g}"
        batch_passages = [rows[i]["product"] for i in idxs]
        batch_scores = sc.score_many(q, batch_passages)
        if batch_scores is None:
            print(f"FAILED on group {g}")
            return
        for i, s in zip(idxs, batch_scores):
            scores[i] = s

    for r, s in zip(rows, scores):
        r["_score"] = s

    cons_scores = [r["_score"] for r in rows if r["label"] == LABEL_CONSISTENT]
    cont_scores = [r["_score"] for r in rows if r["label"] == LABEL_CONTRADICTS]
    labels_bin = [1 if r["label"] == LABEL_CONSISTENT else 0 for r in rows]

    mean_cons = statistics.mean(cons_scores) if cons_scores else 0.0
    mean_cont = statistics.mean(cont_scores) if cont_scores else 0.0
    auc = _roc_auc(scores, labels_bin)
    pair_acc, correct, total = _pairwise_ranking(rows)

    # бинарная accuracy по threshold
    bin_correct = sum(
        int((s >= args.threshold) == (lbl == 1))
        for s, lbl in zip(scores, labels_bin)
    )
    bin_acc = bin_correct / len(rows)

    print("=== METRICS ===")
    print(
        f"  mean score consistent : {mean_cons:.3f}  (n={len(cons_scores)})"
    )
    print(
        f"  mean score contradicts: {mean_cont:.3f}  (n={len(cont_scores)})"
    )
    print(f"  separation             : {mean_cons - mean_cont:+.3f}")
    print(f"  ROC-AUC                : {auc:.3f}")
    print(
        f"  pairwise ranking acc   : {pair_acc:.3f}  "
        f"({correct}/{total} correct pairs внутри групп)"
    )
    print(
        f"  binary acc (thr={args.threshold})  : {bin_acc:.3f}  "
        f"({bin_correct}/{len(rows)})"
    )

    if args.print_failures:
        print("\n=== FAILURES (внутри группы consistent ≤ contradicts) ===")
        by_group_rows: dict[str, list[dict]] = defaultdict(list)
        for r in rows:
            by_group_rows[r["group"]].append(r)
        for group, group_rows in by_group_rows.items():
            cons = [r for r in group_rows if r["label"] == LABEL_CONSISTENT]
            cont = [r for r in group_rows if r["label"] == LABEL_CONTRADICTS]
            failures = []
            for c in cons:
                for x in cont:
                    if c["_score"] <= x["_score"]:
                        failures.append((c, x))
            if not failures:
                continue
            print(f"\n[{group}] query: {group_rows[0]['query']!r}")
            for c, x in failures:
                print(
                    f"  ! consistent  {c['_score']:.3f}  {c['product'][:60]}\n"
                    f"    contradicts {x['_score']:.3f}  {x['product'][:60]}"
                )


if __name__ == "__main__":
    main()
