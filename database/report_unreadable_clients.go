package database

import (
	"log"

	"github.com/shenaba/2s-ui/database/jsonsql"

	"gorm.io/gorm"
)

// reportUnreadableClients names the clients whose config or inbound list is
// not valid JSON.
//
// Every query over those columns leaves such a row out instead of failing on
// it (see jsonsql): a client with an unreadable config is on no inbound's user
// list, and one with an unreadable inbound list is attached to nothing. That
// keeps one damaged row from taking config generation, and the core, down --
// and it is silent, so this is where the operator hears about it: the client
// cannot connect, nothing else is wrong, and nothing else would say why.
//
// The panel writes both columns from parsed JSON, so a row like this comes
// from outside it -- a restored backup, an edit by hand. Nothing here repairs
// it: what the row was meant to hold is not ours to guess.
//
// It reports and never fails. A start that cannot run this query has lost a
// log line, not a reason to stop.
func reportUnreadableClients() {
	for _, problem := range unreadableClients(db) {
		log.Print("clients: ", problem)
	}
}

func unreadableClients(tx *gorm.DB) []string {
	var problems []string
	for _, column := range []struct{ name, effect string }{
		{"config", "it is left off every inbound's user list"},
		{"inbounds", "it is treated as attached to no inbound"},
	} {
		var names []string
		// An empty value is not JSON either and reads the same way, but it
		// says nothing was stored, not that something was damaged.
		err := tx.Raw("SELECT name FROM clients WHERE TRIM(" + jsonsql.Text(column.name) + ") <> '' AND NOT " +
			jsonsql.Valid(column.name) + " ORDER BY id").Scan(&names).Error
		if err != nil {
			log.Printf("clients: could not check the %s column for unreadable rows: %v", column.name, err)
			continue
		}
		for _, name := range names {
			problems = append(problems, "the "+column.name+" of client \""+name+"\" is not valid JSON, so "+
				column.effect+"; the row has to be corrected by hand")
		}
	}
	return problems
}
