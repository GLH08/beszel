package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Makes the monitors (host, port) index unique so the same host:port cannot be
// added twice (which would ping it redundantly). Existing duplicates are
// collapsed first (keep the oldest per host:port, delete the rest) so the
// unique index can be created without a constraint violation.
func init() {
	m.Register(func(app core.App) error {
		// Collapse existing duplicates: for each (host, port) keep the oldest
		// record (lowest created), delete the rest.
		records, err := app.FindRecordsByFilter("monitors", "", "created", 0, 0, nil)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, r := range records {
			key := r.GetString("host") + ":" + r.GetString("port")
			if seen[key] {
				_ = app.Delete(r) // best-effort; a leftover dup surfaces below
				continue
			}
			seen[key] = true
		}

		col, err := app.FindCachedCollectionByNameOrId("monitors")
		if err != nil {
			return err
		}
		// Update the collection model (AddIndex removes the prior index of the
		// same name first). app.Save triggers schema sync, but to be certain the
		// existing non-unique SQL index is replaced, also drop/recreate it via
		// raw DDL — Save's sync does not always rebuild an already-present index.
		col.AddIndex("idx_monitors_host_port", true, "`host`, `port`", "")
		if err := app.Save(col); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery("DROP INDEX IF EXISTS `idx_monitors_host_port`").Execute(); err != nil {
			return err
		}
		_, err = app.DB().NewQuery("CREATE UNIQUE INDEX IF NOT EXISTS `idx_monitors_host_port` ON `monitors` (`host`, `port`)").Execute()
		return err
	}, func(app core.App) error {
		// Down: revert to non-unique (best-effort).
		col, err := app.FindCachedCollectionByNameOrId("monitors")
		if err != nil {
			return err
		}
		col.AddIndex("idx_monitors_host_port", false, "`host`, `port`", "")
		if err := app.Save(col); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery("DROP INDEX IF EXISTS `idx_monitors_host_port`").Execute(); err != nil {
			return err
		}
		_, err = app.DB().NewQuery("CREATE INDEX IF NOT EXISTS `idx_monitors_host_port` ON `monitors` (`host`, `port`)").Execute()
		return err
	})
}
