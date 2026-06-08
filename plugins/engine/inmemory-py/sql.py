"""The deliberately tiny mini-SQL parser — Python mirror of the Go reference's
sql.go. The SAME three regexes are used in both languages so the two independent
parsers stay in lockstep on the shared golden vectors.

Grammar (case-sensitive keywords, single-space separated):
    CREATE TABLE <t> (<col>, <col>, ...)
    INSERT INTO <t> VALUES (<v>, <v>, ...)
    SELECT <* | col, col, ...> FROM <t> [WHERE <col> = <val>] [LIMIT <n>]

Identifiers + values are [A-Za-z0-9_]+. Anything else is a parse error.
"""

import re
from dataclasses import dataclass, field
from typing import List, Optional

_CREATE = re.compile(r"^CREATE TABLE (\w+) \(([\w, ]+)\)$")
_INSERT = re.compile(r"^INSERT INTO (\w+) VALUES \(([\w, ]+)\)$")
_SELECT = re.compile(r"^SELECT (\*|[\w, ]+) FROM (\w+)( WHERE (\w+) = (\w+))?( LIMIT (\d+))?$")


class SQLError(Exception):
    """Any mini-SQL problem → INVALID_ARGUMENT at the RPC boundary."""


@dataclass
class Stmt:
    kind: str  # "create" | "insert" | "select"
    table: str
    cols: List[str] = field(default_factory=list)  # create cols / select projection (["*"] = all)
    vals: List[str] = field(default_factory=list)  # insert values
    where_col: Optional[str] = None
    where_val: Optional[str] = None
    has_where: bool = False
    limit: int = 0
    has_limit: bool = False


def _split_list(s: str) -> List[str]:
    return s.split(", ")


def parse_sql(sql: str) -> Stmt:
    sql = sql.strip()
    if not sql:
        raise SQLError("sql is required")
    m = _CREATE.match(sql)
    if m:
        return Stmt(kind="create", table=m.group(1), cols=_split_list(m.group(2)))
    m = _INSERT.match(sql)
    if m:
        return Stmt(kind="insert", table=m.group(1), vals=_split_list(m.group(2)))
    m = _SELECT.match(sql)
    if m:
        st = Stmt(kind="select", table=m.group(2))
        st.cols = ["*"] if m.group(1) == "*" else _split_list(m.group(1))
        if m.group(3):  # " WHERE col = val"
            st.has_where, st.where_col, st.where_val = True, m.group(4), m.group(5)
        if m.group(6):  # " LIMIT n"
            st.has_limit, st.limit = True, int(m.group(7))
        return st
    raise SQLError("unparseable sql (mini-SQL grammar only)")
