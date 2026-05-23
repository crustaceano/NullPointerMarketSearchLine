"""Sanity check для expanded query.

После отказа от concept-DB условий стало два:
  * запрос не пустой;
  * каждый защищённый regex-span (число / артикул / размер) сохранился
    дословно. Это критично: если SymSpell или WordNet случайно поменяет
    `r17` на что-то другое, мы потеряем точное совпадение по фильтру в
    маркетплейсе.

Возвращаем `(ok, reasons)`: пустой список причин == всё ок.
"""

from __future__ import annotations

from .schema import ParsedQuery


def validate_expanded_query(
    expanded: str,
    parsed: ParsedQuery,
) -> tuple[bool, list[str]]:
    reasons: list[str] = []
    text = (expanded or "").strip().lower()
    if not text:
        return False, ["empty query"]

    for span in parsed.protected_terms:
        if span.text.lower() not in text:
            reasons.append(f"lost protected {span.kind}: {span.text!r}")

    return (not reasons), reasons
