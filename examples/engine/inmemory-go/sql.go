// sql.go — the deliberately tiny mini-SQL parser.
//
// The grammar is intentionally minimal + rigidly canonical so that two
// independent reference implementations (this and inmemory-py) parse identically
// and the golden vectors stay in lockstep. The SAME three regexes are used in both
// languages. Grammar (case-sensitive keywords, single-space separated):
//
//	CREATE TABLE <t> (<col>, <col>, ...)
//	INSERT INTO <t> VALUES (<v>, <v>, ...)
//	SELECT <* | col, col, ...> FROM <t> [WHERE <col> = <val>] [LIMIT <n>]
//
// Identifiers + values are [A-Za-z0-9_]+. Anything else is a parse error →
// INVALID_ARGUMENT at the RPC boundary. SQL fidelity is NOT the point; the engine
// WIRE contract is.
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type stmt struct {
	kind               string // "create" | "insert" | "select"
	table              string
	cols               []string // create columns / select projection (["*"] = all)
	vals               []string // insert values
	whereCol, whereVal string
	hasWhere           bool
	limit              int64
	hasLimit           bool
}

var (
	reCreate = regexp.MustCompile(`^CREATE TABLE (\w+) \(([\w, ]+)\)$`)
	reInsert = regexp.MustCompile(`^INSERT INTO (\w+) VALUES \(([\w, ]+)\)$`)
	reSelect = regexp.MustCompile(`^SELECT (\*|[\w, ]+) FROM (\w+)( WHERE (\w+) = (\w+))?( LIMIT (\d+))?$`)

	errEmptySQL = errors.New("sql is required")
	errParse    = errors.New("unparseable sql (mini-SQL grammar only)")
)

func errUnknownTable(name string) error { return fmt.Errorf("unknown table %q", name) }

// splitList splits a ", "-separated list (CREATE cols / INSERT vals / projection).
func splitList(s string) []string { return strings.Split(s, ", ") }

func parseSQL(sql string) (stmt, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return stmt{}, errEmptySQL
	}
	if m := reCreate.FindStringSubmatch(sql); m != nil {
		return stmt{kind: "create", table: m[1], cols: splitList(m[2])}, nil
	}
	if m := reInsert.FindStringSubmatch(sql); m != nil {
		return stmt{kind: "insert", table: m[1], vals: splitList(m[2])}, nil
	}
	if m := reSelect.FindStringSubmatch(sql); m != nil {
		st := stmt{kind: "select", table: m[2]}
		if m[1] == "*" {
			st.cols = []string{"*"}
		} else {
			st.cols = splitList(m[1])
		}
		if m[3] != "" { // " WHERE col = val"
			st.hasWhere, st.whereCol, st.whereVal = true, m[4], m[5]
		}
		if m[6] != "" { // " LIMIT n"
			n, err := strconv.ParseInt(m[7], 10, 64)
			if err != nil {
				return stmt{}, errParse
			}
			st.hasLimit, st.limit = true, n
		}
		return st, nil
	}
	return stmt{}, errParse
}
