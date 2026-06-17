package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Adds per-system monthly traffic quota config and a traffic_monthly collection
// that stores cumulative bytes per billing cycle (see internal/hub/systems/traffic.go).
func init() {
	m.Register(func(app core.App) error {
		// add quota + billing-cycle reset day to the systems collection
		sysCol, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		sysCol.Fields.Add(&core.NumberField{
			Name:     "traffic_quota",
			Min:      types.Pointer(0.0),
			OnlyInt:  true,
		})
		sysCol.Fields.Add(&core.NumberField{
			Name:     "traffic_reset_day",
			Min:      types.Pointer(1.0),
			Max:      types.Pointer(28.0),
			OnlyInt:  true,
		})
		if err := app.Save(sysCol); err != nil {
			return err
		}

		// traffic_monthly: one row per (system, billing-cycle period)
		col := core.NewBaseCollection("traffic_monthly")
		col.Fields.Add(&core.RelationField{
			Name:          "system",
			CollectionId:  sysCol.Id,
			CascadeDelete: true,
			MaxSelect:     1,
			Required:      true,
		})
		col.Fields.Add(&core.TextField{
			Name:     "period",
			Required: true,
			Max:      10, // "2006-01-02"
		})
		col.Fields.Add(&core.NumberField{Name: "bytes_up", OnlyInt: true})
		col.Fields.Add(&core.NumberField{Name: "bytes_down", OnlyInt: true})
		// last_* hold the previous cumulative counter values used to compute deltas
		col.Fields.Add(&core.NumberField{Name: "last_up", OnlyInt: true})
		col.Fields.Add(&core.NumberField{Name: "last_down", OnlyInt: true})
		// notified is set once per period after the quota-exceeded notification fires
		col.Fields.Add(&core.BoolField{Name: "notified"})
		col.AddIndex("idx_traffic_monthly_period", true, "`system`, `period`", "")
		return app.Save(col)
	}, func(app core.App) error {
		return nil
	})
}
