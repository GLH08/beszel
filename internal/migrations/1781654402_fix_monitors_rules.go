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
