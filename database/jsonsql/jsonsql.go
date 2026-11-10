// Package jsonsql writes the SQLite JSON function calls over stored columns.
//
// The JSON columns are json.RawMessage, a []byte, so the driver stores them as
// BLOB. Since 3.45 SQLite reads a BLOB handed to a JSON function as JSONB when
// its header happens to fit its length, and the first byte of the text decides
// that: "[" is the JSONB header of a 5-byte array, so every 6-byte array --
// "[1, 2]", "[1,23]", "[1234]" -- is parsed as JSONB and fails with "malformed
// JSON" ("{" does the same to every 8-byte object). The failure is the whole
// statement's, not the row's: one such client failed every query that scans
// the table, and with it config generation, so the core could not start
// (issue #268).
//
// Every call here reads the column through CAST(... AS TEXT), whatever the
// driver stored. The rows themselves stay BLOB; only how a query reads them
// changes, so rows written before the fix read correctly too. Nothing outside
// this package spells a JSON function, or the -> and ->> operators that read a
// BLOB the same way, in SQL -- TestNoBareJSONFunctions holds the rest of the
// tree to that.
//
// # A row that is not JSON
//
// The same failure has a second cause, and the cast does nothing for it: a
// column that really does not hold JSON -- a row restored from a damaged
// backup, or written by hand. json_extract and json_each do not skip such a
// row, they fail the statement, so one client with a broken config or inbound
// list again took config generation, and the core, down with it.
//
// So Extract and Each read a column that is not valid JSON as NULL: Extract
// yields NULL, as for a path the row does not have, and Each yields no rows,
// as for an empty list. The row is left out of the answer and every other row
// is served. Nothing here says which row that was; reportUnreadableClients in
// the database package does, at every start.
//
// json_valid is asked in a CASE, not an AND beside the call, so that the order
// is not the query planner's to choose.
package jsonsql

import "strings"

// Text is col read as JSON text.
func Text(col string) string {
	return "CAST(" + col + " AS TEXT)"
}

// readable is col as JSON text, or NULL where it does not hold valid JSON.
func readable(col string) string {
	return "CASE WHEN " + Valid(col) + " THEN " + Text(col) + " END"
}

// Each is col as a json_each table source. A row whose col is not valid JSON
// contributes no rows.
func Each(col string) string {
	return "json_each(" + readable(col) + ")"
}

// Valid is whether col holds valid JSON text.
func Valid(col string) string {
	return "json_valid(" + Text(col) + ")"
}

// Extract is the value at path in col, and NULL where col is not valid JSON.
// path is built into the SQL, so it must come from code, never from a request;
// a quote in it is a programming error.
func Extract(col, path string) string {
	if strings.ContainsRune(path, '\'') {
		panic("jsonsql: quote in JSON path " + path)
	}
	return "json_extract(" + readable(col) + ", '" + path + "')"
}
