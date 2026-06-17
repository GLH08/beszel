package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Fixes the "monitors" collection access rules. The initial migration used an
// empty-string rule (types.Pointer("")), which PocketBase may normalize to nil
// (admin-only) on save, preventing authenticated users from listing/creating
// ping targets. Switch to the codebase convention: any authenticated user.
//
// (The created/updated autodate fields are added by 1781654403 — this migration
// already ran on deployed DBs before that fix existed, so it can't be re-run
// to add them; migrations are tracked by filename only.)
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
		return app.Save(col)
	}, func(app core.App) error {
		return nil
	})
}
