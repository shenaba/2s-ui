package service

// The JSON columns are json.RawMessage, a []byte, so the driver stores them as
// BLOB. Since 3.45 SQLite reads a BLOB handed to a JSON function as JSONB when
// its header happens to fit its length, and the first byte of the text decides
// that: "[" is the JSONB header of a 5-byte array, so every 6-byte array --
// "[1, 2]", "[1,23]", "[1234]" -- is parsed as JSONB and fails with "malformed
// JSON" ("{" does the same to every 8-byte object). The failure is the whole
// statement's, not the row's: one such client fails every query that scans the
// table, and with it config generation, so the core cannot start (issue #268).
//
// CAST to TEXT makes the text reading explicit whatever the driver stored, and
// repairs rows already written as BLOB without a migration. Every JSON function
// over a stored column goes through one of these.

// clientInboundsEach is the clients.inbounds array as a json_each table source.
const clientInboundsEach = "json_each(CAST(clients.inbounds AS TEXT))"

// clientConfigText is clients.config as text, for json_extract.
const clientConfigText = "CAST(clients.config AS TEXT)"
