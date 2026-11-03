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
package jsonsql

import "strings"

// Text is col read as JSON text.
func Text(col string) string {
	return "CAST(" + col + " AS TEXT)"
}

// Each is col as a json_each table source.
func Each(col string) string {
	return "json_each(" + Text(col) + ")"
}

// Valid is whether col holds valid JSON text.
func Valid(col string) string {
	return "json_valid(" + Text(col) + ")"
}

// Extract is the value at path in col. path is built into the SQL, so it must
// come from code, never from a request; a quote in it is a programming error.
func Extract(col, path string) string {
	if strings.ContainsRune(path, '\'') {
		panic("jsonsql: quote in JSON path " + path)
	}
	return "json_extract(" + Text(col) + ", '" + path + "')"
}
