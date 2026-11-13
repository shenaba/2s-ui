package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
)

func TestAssignAdoptedNodeInbounds(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "assignment.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDBForTest() })

	tests := []struct {
		name    string
		opts    InboundAssignment
		count   int
		a, b    string
		wantErr bool
	}{
		{"all", InboundAssignment{Mode: "all"}, 2, `[10,11]`, `[2,10,11]`, false},
		{"groups", InboundAssignment{Mode: "groups", Groups: []string{"team"}}, 1, `[]`, `[2,10,11]`, false},
		{"no group", InboundAssignment{Mode: "groups", Groups: []string{""}}, 1, `[10,11]`, `[2,10]`, false},
		{"selected", InboundAssignment{Mode: "selected"}, 1, `[10,11]`, `[2,10]`, false},
		{"none", InboundAssignment{Mode: "none"}, 0, `[]`, `[2,10]`, false},
		{"invalid selection", InboundAssignment{Mode: "selected", ClientIDs: []uint{999999}}, 0, `[]`, `[2,10]`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := database.GetDB().Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()

			rows := []model.Client{
				{Name: "ungrouped", Inbounds: nil},
				{Name: "team-client", Group: "team", Inbounds: json.RawMessage(`[2,10]`)},
				{Name: "node-client", Group: "@cluster", Inbounds: json.RawMessage(`[5]`)},
			}
			for i := range rows {
				if err := tx.Create(&rows[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Exec("UPDATE clients SET `group` = NULL WHERE id = ?", rows[0].Id).Error; err != nil {
				t.Fatal(err)
			}

			opts := tc.opts
			if tc.name == "selected" {
				opts.ClientIDs = []uint{rows[0].Id}
			}

			count, err := assignAdoptedNodeInbounds(tx, []uint{10, 11}, opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v; expected error = %v", err, tc.wantErr)
			}
			if count != tc.count {
				t.Fatalf("assigned = %d; expected %d", count, tc.count)
			}

			for i, expected := range []string{tc.a, tc.b, `[5]`} {
				var saved model.Client
				if err := tx.First(&saved, rows[i].Id).Error; err != nil {
					t.Fatal(err)
				}
				var got, want []uint
				if len(saved.Inbounds) > 0 && string(saved.Inbounds) != "null" {
					if err := json.Unmarshal(saved.Inbounds, &got); err != nil {
						t.Fatal(err)
					}
				}
				if err := json.Unmarshal([]byte(expected), &want); err != nil {
					t.Fatal(err)
				}
				if (len(got) > 0 || len(want) > 0) && !reflect.DeepEqual(got, want) {
					t.Errorf("%s: inbounds %v, expected %v", rows[i].Name, got, want)
				}
			}
		})
	}
}
