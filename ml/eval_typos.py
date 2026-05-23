"""Evaluate normalizer on data/marketplace_queries_typos_1000.csv."""

from __future__ import annotations

import csv
import re
import time
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path

from normalizer import Dictionaries, Normalizer

ROOT = Path(__file__).resolve().parent
CSV_PATH = ROOT / "data" / "marketplace_queries_typos_1000.csv"
DICT_DIR = ROOT / "dictionaries"

_WS = re.compile(r"\s+")
_TOK = re.compile(r"[\w/\-]+", re.UNICODE)


@dataclass
class Stats:
    exact: int = 0
    tf1_sum: float = 0.0
    n: int = 0

    def add(self, exact: int, tf1: float) -> None:
        self.exact += exact
        self.tf1_sum += tf1
        self.n += 1

    def report(self) -> str:
        n = self.n or 1
        return (
            f"exact={self.exact / n * 100:5.1f}%  "
            f"tf1={self.tf1_sum / n:.3f}  n={self.n}"
        )


def _norm(s: str) -> str:
    return _WS.sub(" ", (s or "").strip().lower())


def _tokens(s: str) -> list[str]:
    return [m.group(0).lower() for m in _TOK.finditer(s or "")]


def _token_f1(pred: list[str], gold: list[str]) -> float:
    if not pred and not gold:
        return 1.0
    if not pred or not gold:
        return 0.0
    pc, gc = Counter(pred), Counter(gold)
    overlap = sum((pc & gc).values())
    p = overlap / sum(pc.values())
    r = overlap / sum(gc.values())
    return 0.0 if p + r == 0 else 2 * p * r / (p + r)


def _load_rows() -> list[dict[str, str]]:
    with CSV_PATH.open(encoding="utf-8") as f:
        return list(csv.DictReader(f))


def _print_block(title: str, items: dict[str, Stats], *, sort_key=None) -> None:
    print(f"\n=== {title} ===")
    keys = sorted(items, key=sort_key) if sort_key else sorted(items)
    for key in keys:
        print(f"  {key:40s} {items[key].report()}")


def main() -> None:
    rows = _load_rows()
    print(f"dataset: {CSV_PATH.name} ({len(rows)} rows)")

    normalizer = Normalizer(Dictionaries.load(DICT_DIR))
    print(
        f"corrector={normalizer.corrector_name}  "
        f"vocab={normalizer.full_vocab_size}"
    )

    overall = Stats()
    by_category: dict[str, Stats] = defaultdict(Stats)
    by_typo: dict[str, Stats] = defaultdict(Stats)
    by_has_typo: dict[str, Stats] = defaultdict(Stats)
    regressions: list[tuple[str, str, str]] = []
    failures: list[tuple[str, str, str, str]] = []

    t0 = time.perf_counter()
    for i, row in enumerate(rows, 1):
        pred = _norm(str(normalizer.normalize(row["raw_query"])["corrected"]))
        gold = _norm(row["correct_query"])
        exact = int(pred == gold)
        tf1 = _token_f1(_tokens(pred), _tokens(gold))

        for bucket in (
            overall,
            by_category[row["category"]],
            by_typo[row["typo_type"]],
            by_has_typo[row["has_typo"]],
        ):
            bucket.add(exact, tf1)

        if row["has_typo"] == "0" and not exact:
            regressions.append((row["raw_query"], pred, gold))
        elif not exact:
            failures.append((row["typo_type"], row["raw_query"], pred, gold))

        if i % 250 == 0:
            print(f"  {i}/{len(rows)}")

    elapsed = time.perf_counter() - t0
    qps = len(rows) / elapsed if elapsed else 0

    print(f"\n=== OVERALL ===\n  {overall.report()}")
    print(f"  {elapsed:.2f}s, {qps:.0f} q/s")

    _print_block(
        "BY HAS_TYPO",
        {
            "clean (has_typo=0)": by_has_typo["0"],
            "noisy (has_typo=1)": by_has_typo["1"],
        },
        sort_key=lambda k: k,
    )
    _print_block("BY CATEGORY", by_category)
    _print_block("BY TYPO TYPE", by_typo, sort_key=lambda k: -by_typo[k].n)

    print(f"\n=== REGRESSIONS ===\n  total: {len(regressions)} / {by_has_typo['0'].n}")
    for raw, pred, gold in regressions[:10]:
        print(f"  raw : {raw}\n  pred: {pred}\n  gold: {gold}\n")

    print(f"=== FAILURES (noisy) ===\n  total: {len(failures)} / {by_has_typo['1'].n}")
    for typo, raw, pred, gold in failures[:10]:
        print(f"  [{typo}]\n  raw : {raw}\n  pred: {pred}\n  gold: {gold}\n")


if __name__ == "__main__":
    main()
