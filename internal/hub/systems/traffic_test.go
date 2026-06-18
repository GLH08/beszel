//go:build testing

package systems

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestComputeNetDelta covers per-interface traffic delta computation: the fix
// for monthly traffic overcounting when a NIC resets/added/removed. The old
// sum-then-compare approach mis-handled these; per-interface delta is correct.
func TestComputeNetDelta(t *testing.T) {
	cases := []struct {
		name                string
		current             map[string][4]uint64
		last                map[string][2]uint64
		wantSent, wantRecv  uint64
		wantLast            map[string][2]uint64
	}{
		{
			name:     "nil baseline seeds everything, zero delta",
			current:  map[string][4]uint64{"eth0": {0, 0, 1000, 2000}},
			last:     nil,
			wantSent: 0, wantRecv: 0,
			wantLast: map[string][2]uint64{"eth0": {1000, 2000}},
		},
		{
			name:     "single nic growth",
			current:  map[string][4]uint64{"eth0": {0, 0, 1500, 3000}},
			last:     map[string][2]uint64{"eth0": {1000, 2000}},
			wantSent: 500, wantRecv: 1000,
			wantLast: map[string][2]uint64{"eth0": {1500, 3000}},
		},
		{
			name:     "single nic reset (reboot): new smaller value is the delta",
			current:  map[string][4]uint64{"eth0": {0, 0, 50, 80}},
			last:     map[string][2]uint64{"eth0": {1000, 2000}},
			wantSent: 50, wantRecv: 80,
			wantLast: map[string][2]uint64{"eth0": {50, 80}},
		},
		{
			name:     "new nic added: seeded, no delta this cycle",
			current:  map[string][4]uint64{"eth0": {0, 0, 1500, 3000}, "eth1": {0, 0, 9999, 9999}},
			last:     map[string][2]uint64{"eth0": {1000, 2000}},
			wantSent: 500, wantRecv: 1000, // only eth0's delta; eth1 seeded
			wantLast: map[string][2]uint64{"eth0": {1500, 3000}, "eth1": {9999, 9999}},
		},
		{
			name:     "nic removed: dropped from baseline, no delta",
			current:  map[string][4]uint64{"eth0": {0, 0, 1500, 3000}},
			last:     map[string][2]uint64{"eth0": {1000, 2000}, "eth1": {500, 500}},
			wantSent: 500, wantRecv: 1000,
			wantLast: map[string][2]uint64{"eth0": {1500, 3000}}, // eth1 gone
		},
		{
			name: "two nics, one resets: per-nic correct (the core bug)",
			// eth0 grows 1000->1500 (delta 500), eth1 resets 400->50 (delta 50).
			// Old summed approach: last sum=1400, current sum=1550 -> delta 150 (wrong;
			// should be 550). Per-nic: 500 + 50 = 550. Correct.
			current:  map[string][4]uint64{"eth0": {0, 0, 1500, 0}, "eth1": {0, 0, 50, 0}},
			last:     map[string][2]uint64{"eth0": {1000, 0}, "eth1": {400, 0}},
			wantSent: 550, wantRecv: 0,
			wantLast: map[string][2]uint64{"eth0": {1500, 0}, "eth1": {50, 0}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotSent, gotRecv, gotLast := computeNetDelta(c.current, c.last)
			require.Equal(t, c.wantSent, gotSent, "sent delta")
			require.Equal(t, c.wantRecv, gotRecv, "recv delta")
			require.Equal(t, c.wantLast, gotLast, "updated baseline")
		})
	}
}
