"""Eval WordNet-based query expansion на размеченном golden set.

Запуск:
    python ml/eval_expand.py
    python ml/eval_expand.py --print-failures
    python ml/eval_expand.py --no-typo-branch          # без SymSpell-ветки

Метрики (по агрегату):
  protect_p / r / f1  — сохраняем regex-protected при типизации (числа,
                        артикулы, размеры). Точное совпадение по text.
  has_synonyms_pct    — % примеров, где для запроса найден ≥1 расширяемый
                        токен с непустым WordNet-списком. Прокси «WordNet
                        вообще что-то знает в этом домене».
  expansion_pass      — % примеров с valid_expansions ≥ min_valid_expansions
                        (минимум — сам corrected, поэтому почти всегда ≥1).
  expansions_per_q    — среднее число valid expansions per query
                        (чем больше — тем шире recall).
  diversity_avg       — доля уникальных слов в expansions относительно
                        исходного запроса (чем выше — тем разнообразнее
                        синонимизация).
  per_example_pass    — % примеров без ошибок (protected F1=1, expansions ok).

Golden — append-only `data/expand/golden.jsonl`. ОБЕЩАНО: не подгонять.
Если тест нашёл косяк — чиним pipeline или принимаем как known limitation,
но не правим golden.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

try:
    sys.stdout.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
except Exception:
    pass

sys.path.insert(0, str(Path(__file__).resolve().parent))

from normalizer import Dictionaries, Normalizer  # noqa: E402
from synonyms import (  # noqa: E402
    ProtectedSpanFinder,
    QueryParser,
    generate_expanded_queries,
    get_shared_wordnet,
    validate_expanded_query,
)


BASE = Path(__file__).resolve().parent
GOLDEN_DEFAULT = BASE / "data" / "expand" / "golden.jsonl"


def load_golden(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open(encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("//"):
                continue
            rows.append(json.loads(line))
    return rows


def prf(tp: int, fp: int, fn: int) -> tuple[float, float, float]:
    p = tp / (tp + fp) if (tp + fp) else 0.0
    r = tp / (tp + fn) if (tp + fn) else 0.0
    f = (2 * p * r / (p + r)) if (p + r) else 0.0
    return p, r, f


def diversity(query: str, expansions: list[str]) -> float:
    """Доля «новых» слов в expansions относительно исходного запроса.

    `0.0` — все expansions идентичны базовому запросу;
    `1.0` — каждое расширение полностью из новых слов.
    """
    base_words = set(query.lower().split())
    if not expansions:
        return 0.0
    novel_total = 0
    total = 0
    for q in expansions:
        words = set(q.lower().split())
        total += len(words)
        novel_total += len(words - base_words)
    return novel_total / total if total else 0.0


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--golden", default=str(GOLDEN_DEFAULT))
    ap.add_argument("--print-failures", action="store_true")
    ap.add_argument(
        "--no-typo-branch",
        action="store_true",
        help="не запускать SymSpell-ветку, считать метрики только по raw",
    )
    args = ap.parse_args()

    rows = load_golden(Path(args.golden))
    print(f"loaded: {len(rows)} examples from {args.golden}")

    norm = Normalizer(Dictionaries.load(BASE / "dictionaries"))
    wn = get_shared_wordnet()

    parser = QueryParser(
        wordnet=wn,
        protector=ProtectedSpanFinder(),
        normalizer=norm,
        apply_typo_correction=False,
    )

    typo_branch = not args.no_typo_branch
    print(f"source: {wn.name}, typo_branch={'on' if typo_branch else 'off'}\n")

    prot_tp = prot_fp = prot_fn = 0
    has_syn_count = 0
    exp_ok_count = 0
    pass_count = 0
    total_expansions = 0
    total_diversity = 0.0
    failures: list[dict[str, Any]] = []

    for row in rows:
        if typo_branch:
            parsed_raw, parsed_typo = parser.parse_dual(row["raw"])
        else:
            parsed_raw, parsed_typo = parser.parse(row["raw"]), None

        # union кандидатов от обеих веток + валидация «своим» parsed.
        seen_q: set[str] = set()
        valid_count = 0
        all_expanded: list[str] = []
        for parsed in (parsed_raw, parsed_typo):
            if parsed is None:
                continue
            for q in generate_expanded_queries(parsed, max_queries=8):
                key = q.strip().lower()
                if not key or key in seen_q:
                    continue
                seen_q.add(key)
                all_expanded.append(q)
                if validate_expanded_query(q, parsed)[0]:
                    valid_count += 1

        # protected — по text (regex выдаёт kind, но точные тексты надёжнее).
        got_prot = {p.text.lower() for p in parsed_raw.protected_terms}
        exp_prot = {t.lower() for t in row.get("expected_protected_texts", [])}
        p_tp = len(got_prot & exp_prot)
        p_fp = len(got_prot - exp_prot)
        p_fn = len(exp_prot - got_prot)
        prot_tp += p_tp
        prot_fp += p_fp
        prot_fn += p_fn

        # has_synonyms — есть ли вообще что синонимизировать в этом запросе?
        has_syn_raw = any(t.synonyms for t in parsed_raw.expandable)
        has_syn_typo = (
            any(t.synonyms for t in parsed_typo.expandable)
            if parsed_typo is not None
            else False
        )
        if has_syn_raw or has_syn_typo:
            has_syn_count += 1

        min_exp = int(row.get("min_valid_expansions", 1))
        exp_ok = valid_count >= min_exp
        if exp_ok:
            exp_ok_count += 1

        total_expansions += valid_count
        total_diversity += diversity(parsed_raw.corrected, all_expanded)

        full_ok = p_fp == 0 and p_fn == 0 and exp_ok
        if full_ok:
            pass_count += 1
        else:
            failures.append(
                {
                    "id": row.get("id"),
                    "raw": row["raw"],
                    "got_protected": sorted(got_prot),
                    "exp_protected": sorted(exp_prot),
                    "valid_expansions": valid_count,
                    "min_valid_expansions": min_exp,
                    "expanded": all_expanded,
                }
            )

    n = len(rows) or 1
    pp, pr, pf = prf(prot_tp, prot_fp, prot_fn)

    print("=== METRICS ===")
    print(
        f"  protected P/R/F1   : {pp:.2f} / {pr:.2f} / {pf:.2f}    "
        f"(tp={prot_tp}, fp={prot_fp}, fn={prot_fn})"
    )
    print(f"  has_synonyms_pct   : {has_syn_count / n:.2f}  ({has_syn_count}/{n})")
    print(f"  expansion_pass     : {exp_ok_count / n:.2f}  ({exp_ok_count}/{n})")
    print(f"  expansions_per_q   : {total_expansions / n:.2f}")
    print(f"  diversity_avg      : {total_diversity / n:.2f}")
    print(f"  per_example_pass   : {pass_count / n:.2f}  ({pass_count}/{n})")

    if args.print_failures and failures:
        print("\n=== FAILURES ===")
        for fail in failures:
            print(f"\n[{fail['id']}] {fail['raw']!r}")
            if set(fail["got_protected"]) != set(fail["exp_protected"]):
                print(
                    f"  protected: got {fail['got_protected']}  "
                    f"vs  exp {fail['exp_protected']}"
                )
            if fail["valid_expansions"] < fail["min_valid_expansions"]:
                print(
                    f"  expansions: {fail['valid_expansions']} < "
                    f"{fail['min_valid_expansions']}"
                )
            if fail["expanded"]:
                print("  candidates:")
                for e in fail["expanded"][:6]:
                    print(f"    - {e}")


if __name__ == "__main__":
    main()
