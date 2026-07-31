package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds per-system expiry tracking fields to the systems collection:
// expire_type ("" | permanent | fixed), expire_start, expire_end (dates),
// renew_cycle (month | year). All optional / additive. See
// docs/superpowers/specs/2026-07-31-server-expiry-design.md
func init() {
	m.Register(func(app core.App) error {
		sysCol, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		// expire_type: "" = not configured, "permanent" = no expiry, "fixed" = has end date
		sysCol.Fields.Add(&core.SelectField{
			Name:   "expire_type",
			Values: []string{"permanent", "fixed"},
		})
		sysCol.Fields.Add(&core.DateField{
			Name: "expire_start",
		})
		sysCol.Fields.Add(&core.DateField{
			Name: "expire_end",
		})
		// renew_cycle: only meaningful when expire_type == "fixed"; UI defaults to "month"
		sysCol.Fields.Add(&core.SelectField{
			Name:   "renew_cycle",
			Values: []string{"month", "year"},
		})
		return app.Save(sysCol)
	}, func(app core.App) error {
		return nil
	})
}
