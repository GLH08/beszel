package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds created/updated autodate fields to the "monitors" collection.
//
// The original 1781654401 migration omitted them. On fresh installs that
// migration now adds them directly, but on DBs deployed before that edit the
// collection still lacks the fields — and 1781654401 can't re-run (migrations
// are tracked by filename only). The frontend lists monitors with
// sort="created", which the API rejects with 400 "invalid sort field" when
// "created" isn't a registered field, so the Ping Targets list and home latency
// table appeared empty. This new migration (new filename => new key) runs on
// those already-deployed DBs. Idempotent: only adds fields that are missing.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("monitors")
		if err != nil {
			return nil // collection not present; nothing to update
		}
		if f, _ := col.Fields.GetByName("created").(*core.AutodateField); f == nil {
			col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		}
		if f, _ := col.Fields.GetByName("updated").(*core.AutodateField); f == nil {
			col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		}
		return app.Save(col)
	}, func(app core.App) error {
		return nil
	})
}
