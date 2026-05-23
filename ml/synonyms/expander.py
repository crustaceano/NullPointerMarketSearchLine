"""Генерация expanded queries из ParsedQuery (WordNet-source).

Стратегия — `variant-at-a-time`:
  * берём базовый запрос (corrected);
  * для каждого свободного токена с непустым списком WordNet-синонимов
    создаём один альтернативный вариант, заменяя только этот токен;
  * это даёт N вариантов на N расширяемых токенов — без декартова взрыва.

Если расширяемых токенов нет (WordNet не знает ни одного слова) — на
выходе только сам corrected, чтобы клиент мог использовать его как
baseline.
"""

from __future__ import annotations

from .schema import ParsedQuery


def generate_expanded_queries(
    parsed: ParsedQuery,
    max_queries: int = 6,
) -> list[str]:
    if not parsed.corrected.strip():
        return []

    tokens = list(parsed.tokens)
    base = _join(tokens)
    out: list[str] = [base]
    seen: set[str] = {base.lower()}

    expandable_with_syn = [e for e in parsed.expandable if e.synonyms]
    if not expandable_with_syn:
        return out

    # variant-at-a-time: каждый расширяемый токен пробуем по очереди.
    for tok in expandable_with_syn:
        for syn in tok.synonyms:
            cand_tokens = list(tokens)
            # ExpandableToken — однословный (start+1 == end), но для
            # надёжности оперируем диапазоном.
            cand_tokens[tok.start : tok.end] = [syn]
            q = _join(cand_tokens)
            key = q.lower()
            if key in seen:
                continue
            seen.add(key)
            out.append(q)
            if len(out) >= max_queries:
                return out

    return out


def _join(parts: list[str]) -> str:
    return " ".join(p.strip() for p in parts if p and p.strip())
