package systems

import (
	"context"
	"log/slog"
	"time"

	"github.com/henrygd/beszel/internal/common"
	"github.com/pocketbase/pocketbase/core"
)

// loadMonitorConfig reads all enabled monitors from the DB and builds the
// MonitorConfig payload pushed to agents.
func loadMonitorConfig(app core.App) common.MonitorConfig {
	records, err := app.FindRecordsByFilter(
		"monitors", "enabled = true", "", 0, 0, nil,
	)
	if err != nil {
		slog.Error("load monitors", "err", err)
		return common.MonitorConfig{}
	}
	targets := make([]common.PingTarget, 0, len(records))
	for _, r := range records {
		host := r.GetString("host")
		if host == "" {
			continue
		}
		targets = append(targets, common.PingTarget{
			Id:   r.Id,
			Host: host,
			Port: uint16(r.GetInt("port")),
		})
	}
	return common.MonitorConfig{PingTargets: targets}
}

// configPusher sends the monitor config to a single system. It is an interface
// so tests can verify PushConfigToAll attempts every system (including those
// with no WebSocket connection) without standing up real transports.
type configPusher interface {
	PushConfig(sys *System, cfg common.MonitorConfig)
}

// PushConfigToAll sends the current monitor config to every connected agent.
// Called when monitors are created/updated/deleted. Systems are NOT filtered by
// transport: sys.request falls back from WebSocket to SSH, so an agent
// reachable only via SSH still receives ping targets.
func (sm *SystemManager) PushConfigToAll() {
	cfg := loadMonitorConfig(sm.hub)
	for _, sys := range sm.systems.Values() {
		sm.configPusher.PushConfig(sys, cfg)
	}
}

// PushConfig is the real configPusher implementation: pushes to one agent in a
// goroutine with a 5s timeout, logging failures at debug.
func (sm *SystemManager) PushConfig(sys *System, cfg common.MonitorConfig) {
	go sm.pushConfigToSystem(sys, cfg)
}

// SetConfigPusher overrides the config pusher. Test-only hook; production code
// uses the default (sm.PushConfig).
func (sm *SystemManager) SetConfigPusher(p configPusher) {
	sm.configPusher = p
}

// pushConfigToSystem sends the monitor config to one agent and logs failures.
func (sm *SystemManager) pushConfigToSystem(sys *System, cfg common.MonitorConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ack common.ConfigAck
	if err := sys.request(ctx, common.SetConfig, cfg, &ack); err != nil {
		sm.hub.Logger().Debug("push monitor config failed", "system", sys.Id, "err", err)
	}
}
