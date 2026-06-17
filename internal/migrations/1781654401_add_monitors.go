package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Adds a global "monitors" collection holding latency-test (ping) targets that
// the hub pushes to all agents. Targets are global (shared by all systems),
// like Nezha probe targets.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("monitors")
		// any authenticated user can manage ping targets (shared globally)
		rule := types.Pointer("@request.auth.id != \"\"")
		col.ListRule = rule
		col.ViewRule = rule
		col.CreateRule = rule
		col.UpdateRule = rule
		col.DeleteRule = rule
		col.Fields.Add(&core.TextField{
			Name:     "name",
			Required: true,
			Max:      100,
		})
		col.Fields.Add(&core.TextField{
			Name:     "host",
			Required: true,
			Max:      255,
		})
		col.Fields.Add(&core.NumberField{
			Name:    "port",
			Min:     types.Pointer(1.0),
			Max:     types.Pointer(65535.0),
			OnlyInt: true,
		})
		col.Fields.Add(&core.BoolField{Name: "enabled"})
		col.AddIndex("idx_monitors_host_port", false, "`host`, `port`", "")
		return app.Save(col)
	}, func(app core.App) error {
		return nil
	})
}
