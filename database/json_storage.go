package database

import (
	"encoding/json"
	"log"
	"reflect"

	"gorm.io/gorm"
)

var rawMessageType = reflect.TypeOf(json.RawMessage{})

// normalizeJSONStorage rewrites every JSON column value stored as TEXT to the
// BLOB the panel itself writes.
//
// The JSON columns are json.RawMessage, a []byte, and database/sql scans a
// TEXT value into one only for a plain []byte: for any other byte-slice type
// it fails the whole query with "unsupported Scan, storing driver.Value type
// string". The panel never writes TEXT, but a row edited with the sqlite3
// shell, written by a script or brought in by an import is TEXT, and one such
// row failed every query that loads its table. Run at every start, that took
// the panel down with it: a client row stored as TEXT stopped the mieru
// backfill, and InitDB with it.
//
// The bytes stay as they are; only the storage class changes. A value that is
// not JSON stays not JSON, and is jsonsql's and reportUnreadableClients'
// business. A start with nothing stored as TEXT costs one UPDATE per column
// that changes nothing.
func normalizeJSONStorage(tx *gorm.DB, models ...interface{}) error {
	for _, m := range models {
		stmt := &gorm.Statement{DB: tx}
		if err := stmt.Parse(m); err != nil {
			return err
		}
		table := stmt.Schema.Table
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" || field.FieldType != rawMessageType {
				continue
			}
			column := stmt.Quote(field.DBName)
			result := tx.Exec("UPDATE " + stmt.Quote(table) + " SET " + column + " = CAST(" + column +
				" AS BLOB) WHERE typeof(" + column + ") = 'text'")
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				log.Printf("database: %d value(s) of %s.%s were stored as TEXT, rewritten as BLOB",
					result.RowsAffected, table, field.DBName)
			}
		}
	}
	return nil
}
