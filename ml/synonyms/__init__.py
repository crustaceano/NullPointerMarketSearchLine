"""WordNet-based controlled query expansion."""

from .schema import ExpandableToken, ParsedQuery, ProtectedSpan
from .protector import ProtectedSpanFinder
from .wordnet_lookup import WordNetLookup, get_shared_wordnet
from .parser import QueryParser
from .expander import generate_expanded_queries
from .validator import validate_expanded_query


__all__ = [
    "ExpandableToken",
    "ParsedQuery",
    "ProtectedSpan",
    "ProtectedSpanFinder",
    "WordNetLookup",
    "get_shared_wordnet",
    "QueryParser",
    "generate_expanded_queries",
    "validate_expanded_query",
]
