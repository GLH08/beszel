package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Fixes the "monitors" collection:
//  1. Access rules — the initial migration used an empty-string rule
//     (types.Pointer("")), which PocketBase may normalize to nil (admin-only),
//     preventing authenticated users from listing/creating ping targets.
//     Switch to the codebase convention: any authenticated user.
//  2. created/updated autodate fields — the initial migration omitted them, so
//     the frontend's getFullList({ sort: "created" }) failed with 400
//     "invalid sort field".
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("monitors")
		if err != nil {
			return nil // collection not present; nothing to update
		}
		rule := types.Pointer("@request.auth.id != \"\"")
		col.ListRule = rule
		col.ViewRule = rule
		col.CreateRule = rule
		col.UpdateRule = rule
		col.DeleteRule = rule
		// add created/updated autodate fields if missing (idempotent)
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
