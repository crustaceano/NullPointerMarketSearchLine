"""POST /expand — controlled query expansion поверх RuWordNet.

Pipeline (две параллельные ветки → дедуп):
  raw query
    ├─ branch=raw           → ProtectedSpanFinder → WordNet lookup
    └─ branch=typo_corrected → SymSpell normalize → ProtectedSpanFinder → ...
  → expander по каждой ветке → union кандидатов
  → LM-perplexity filter (LM_FILTER_ENABLED=1, default on)
  → validator (своим parsed: проверяет protected regex-spans).

Зачем две ветки:
  SymSpell умеет починить опечатку (`принтир` → `принтер`), но иногда
  ломает редкое слово, корректируя в общерусское. Прокидываем обе
  версии — recall растёт, бредовые варианты отсеиваются LM-фильтром.

Зачем LM-фильтр (а не NLI):
  WordNet-замена даёт два типа шума: (a) явные противоречия (зимний↔летний)
  и (b) грамматический/семантический бред (`баллон зимняя с шипами`).
  NLI хорошо ловит (a), но плохо (b). LM perplexity отлично ловит
  (b) и заодно режет (a) — у несочетаемых слов высокая perplexity.
"""

from __future__ import annotations

from dataclasses import asdict

from fastapi import APIRouter
from pydantic import BaseModel, Field

from lm_filter import get_shared_lm_filter, lm_filter_ratio
from synonyms import generate_expanded_queries, validate_expanded_query
from synonyms.schema import ParsedQuery

from .deps import (
    lm_filter_enabled,
    query_parser,
)


router = APIRouter(tags=["expand"])


class ExpandRequest(BaseModel):
    query: str = Field(..., description="Сырой пользовательский запрос")
    max_queries: int = Field(default=6, ge=1, le=20)


class ExpandableTokenOut(BaseModel):
    text: str
    synonyms: list[str]
    start: int
    end: int


class ProtectedSpanOut(BaseModel):
    text: str
    kind: str
    start: int
    end: int


class ExpandedQuery(BaseModel):
    query: str
    valid: bool
    reasons: list[str] = Field(default_factory=list)
    branch: str = Field(..., description="raw | typo_corrected")
    perplexity: float | None = Field(
        default=None,
        description="Pseudo-PPL расширенной фразы (None если LM-фильтр выкл)",
    )


class ExpandResponse(BaseModel):
    raw: str
    corrected: str = Field(..., description="raw-ветка (без SymSpell)")
    typo_corrected: str | None = Field(
        default=None,
        description="typo-ветка (после SymSpell), если SymSpell что-то поменял",
    )
    expandable_tokens: list[ExpandableTokenOut]
    protected_terms: list[ProtectedSpanOut]
    attributes: dict[str, str]
    expanded_queries: list[ExpandedQuery]


@router.post("/expand", response_model=ExpandResponse)
def expand(req: ExpandRequest) -> ExpandResponse:
    parsed_raw, parsed_typo = query_parser.parse_dual(req.query)

    expanded = _build_expansions(
        parsed_raw, parsed_typo, max_queries=req.max_queries
    )

    if lm_filter_enabled():
        expanded = _apply_lm_filter(expanded, parsed_raw, parsed_typo)

    # expandable_tokens — union по text (raw в приоритете).
    seen_text: set[str] = set()
    tokens_out: list[ExpandableTokenOut] = []
    for parsed in (parsed_raw, parsed_typo):
        if parsed is None:
            continue
        for tok in parsed.expandable:
            key = tok.text.lower()
            if key in seen_text:
                continue
            seen_text.add(key)
            tokens_out.append(
                ExpandableTokenOut(
                    text=tok.text,
                    synonyms=list(tok.synonyms),
                    start=tok.start,
                    end=tok.end,
                )
            )

    return ExpandResponse(
        raw=parsed_raw.raw,
        corrected=parsed_raw.corrected,
        typo_corrected=(parsed_typo.corrected if parsed_typo is not None else None),
        expandable_tokens=tokens_out,
        protected_terms=[
            ProtectedSpanOut(**asdict(p)) for p in parsed_raw.protected_terms
        ],
        attributes=parsed_raw.attributes,
        expanded_queries=expanded,
    )


def _build_expansions(
    parsed_raw: ParsedQuery,
    parsed_typo: ParsedQuery | None,
    max_queries: int,
) -> list[ExpandedQuery]:
    out: list[ExpandedQuery] = []
    seen: set[str] = set()

    branches: list[tuple[str, ParsedQuery]] = [("raw", parsed_raw)]
    if parsed_typo is not None:
        branches.append(("typo_corrected", parsed_typo))

    for branch_name, parsed in branches:
        for q in generate_expanded_queries(parsed, max_queries):
            key = q.strip().lower()
            if not key or key in seen:
                continue
            seen.add(key)
            ok, reasons = validate_expanded_query(q, parsed)
            out.append(
                ExpandedQuery(
                    query=q,
                    valid=ok,
                    reasons=reasons,
                    branch=branch_name,
                )
            )
    return out


def _apply_lm_filter(
    expanded: list[ExpandedQuery],
    parsed_raw: ParsedQuery,
    parsed_typo: ParsedQuery | None,
) -> list[ExpandedQuery]:
    """LM pseudo-PPL фильтр — выкидывает бредовые кандидаты.

    Бейслайн PPL берём с **того же** оригинала, в чьей ветке родился
    кандидат (raw vs typo_corrected): SymSpell может изменить редкое
    слово на частотное и занизить PPL базовой фразы — несправедливо
    штрафовать кандидатов typo-ветки относительно raw-ветки.

    Сами оригиналы (corrected/typo_corrected) всегда сохраняем — их
    выкидывать бессмысленно.
    """
    if not expanded:
        return expanded
    lm = get_shared_lm_filter()
    if lm is None:
        return expanded

    base_raw = parsed_raw.corrected.strip() or parsed_raw.raw.strip()
    base_typo = (
        parsed_typo.corrected.strip()
        if parsed_typo is not None
        else base_raw
    )
    base_set = {base_raw.lower(), base_typo.lower()}

    # Один общий батч: [base_raw, base_typo, *candidates]. Это позволяет
    # делать ровно один call в LMFilter за весь /expand-запрос.
    bases = [base_raw, base_typo] if parsed_typo is not None else [base_raw]
    texts = bases + [c.query for c in expanded]
    ppls = lm.perplexity_many(texts)
    if ppls is None:
        return expanded

    base_raw_ppl = ppls[0]
    base_typo_ppl = ppls[1] if parsed_typo is not None else base_raw_ppl

    ratio = lm_filter_ratio()
    kept: list[ExpandedQuery] = []
    for cand, ppl in zip(expanded, ppls[len(bases):]):
        cand.perplexity = float(ppl)
        baseline = base_typo_ppl if cand.branch == "typo_corrected" else base_raw_ppl
        if cand.query.lower() in base_set:
            kept.append(cand)
            continue
        if not math_ok(ppl, baseline, ratio):
            continue
        kept.append(cand)
    return kept


def math_ok(ppl: float, baseline: float, ratio: float) -> bool:
    """ppl ≤ baseline * ratio с защитой от inf/0/NaN."""
    import math

    if not math.isfinite(ppl) or ppl <= 0:
        return False
    if not math.isfinite(baseline) or baseline <= 0:
        return True  # бейслайн сломан — не штрафуем кандидатов
    return ppl <= baseline * ratio
