//go:build testing

package systems_test

import (
	"sync"
	"testing"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub/systems"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/require"
)

// recordingPusher is a configPusher that records the Id of every system a
// config push was attempted for, regardless of transport. It satisfies the
// unexported systems.configPusher interface structurally (its PushConfig method
// signature matches). Used to prove PushConfigToAll no longer skips systems
// without an active WebSocket connection (the SSH-only-agent fix).
type recordingPusher struct {
	mu     sync.Mutex
	pushed []string
}

func (r *recordingPusher) PushConfig(sys *systems.System, cfg common.MonitorConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushed = append(r.pushed, sys.Id)
}

func (r *recordingPusher) pushedIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.pushed))
	copy(out, r.pushed)
	return out
}

// TestPushConfigToAllIncludesSSHOnlySystems verifies that monitor config is
// pushed to systems without an active WebSocket connection. A freshly added
// system has WsConn == nil (the SSH-only case). Previously PushConfigToAll
// skipped such systems, so agents reachable only via SSH never received ping
// targets and their latency chart was permanently empty. sys.request already
// falls back from WebSocket to SSH, so the only fix needed is to stop
// skipping. With the fix, every system is pushed regardless of WsConn.
func TestPushConfigToAllIncludesSSHOnlySystems(t *testing.T) {
	hub, err := tests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer hub.Cleanup()

	// A monitors record so loadMonitorConfig has something to push.
	_, err = tests.CreateRecord(hub, "monitors", map[string]any{
		"name":    "Google",
		"host":    "8.8.8.8",
		"port":    443,
		"enabled": true,
	})
	require.NoError(t, err)

	sm := hub.GetSystemManager()

	// Two freshly added systems. Neither connects a WebSocket, so both are in
	// the "SSH-only" state (WsConn == nil). AddSystem starts background
	// updaters; they fail against these dummy hosts but PushConfigToAll runs
	// independently and synchronously. Cleanup stops the updaters.
	wsSys := sm.NewSystem("ws-system")
	wsSys.Host = "127.0.0.1"
	sshSys := sm.NewSystem("ssh-system")
	sshSys.Host = "127.0.0.1"
	require.NoError(t, sm.AddSystem(wsSys))
	require.NoError(t, sm.AddSystem(sshSys))

	// Inject the recorder so we observe push attempts without real transports.
	rec := &recordingPusher{}
	sm.SetConfigPusher(rec)

	sm.PushConfigToAll()

	got := rec.pushedIDs()
	require.ElementsMatch(t, []string{"ws-system", "ssh-system"}, got,
		"config must be pushed to systems with no active WebSocket connection")
}
