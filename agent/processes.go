package agent

import (
	"log/slog"
	"sort"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

// topProcessCount is how many processes the snapshot keeps (by CPU usage).
const topProcessCount = 10

// cmdDisplayLimit truncates the command line for display; the full value is
// not sent over the wire to keep payloads small.
const cmdDisplayLimit = 60

// procSample holds the CPU time (user+system seconds) observed for a pid at a
// given instant, used to compute per-process CPU% between two samples.
type procSample struct {
	cpuTime float64
}

// processSampler computes per-process CPU% from successive samples of CPU
// times. It carries state across calls so it only produces meaningful values
// from the second call onward (the first call seeds the baseline).
type processSampler struct {
	last   map[int32]procSample // pid -> previous CPU time
	lastAt time.Time            // when the previous sample was taken
	cores  int                  // logical core count, to scale CPU% above 100 on multi-core
}

func newProcessSampler() *processSampler {
	cores, err := cpu.Counts(true)
	if err != nil || cores <= 0 {
		cores = 1
	}
	return &processSampler{
		last:  make(map[int32]procSample),
		cores: cores,
	}
}

// gatherTopProcesses returns up to topProcessCount processes sorted by CPU%.
// On the first call it seeds the baseline and returns nil (no delta yet).
func (ps *processSampler) gatherTopProcesses() []*system.Process {
	procs, err := process.Processes()
	if err != nil {
		slog.Debug("process list", "err", err)
		return nil
	}

	now := time.Now()
	var elapsed time.Duration
	if !ps.lastAt.IsZero() {
		elapsed = now.Sub(ps.lastAt)
	}

	type entry struct {
		p   *system.Process
		cpu float64
	}
	entries := make([]entry, 0, len(procs))
	nextLast := make(map[int32]procSample, len(procs))

	for _, p := range procs {
		pid := p.Pid

		times, err := p.Times()
		if err != nil {
			continue
		}
		curTime := times.User + times.System
		nextLast[pid] = procSample{cpuTime: curTime}

		// Need a previous sample to compute a delta.
		prev, ok := ps.last[pid]
		if !ok || elapsed <= 0 {
			continue
		}
		if curTime < prev.cpuTime {
			// process was replaced / counters reset; skip this round
			continue
		}
		delta := curTime - prev.cpuTime
		cpuPct := (delta / elapsed.Seconds()) * 100 / float64(ps.cores)
		if cpuPct < 0 {
			cpuPct = 0
		}

		memPct, err := p.MemoryPercent()
		if err != nil {
			memPct = 0
		}

		user, _ := p.Username()
		cmd, _ := p.Cmdline()
		if len(cmd) > cmdDisplayLimit {
			cmd = cmd[:cmdDisplayLimit]
		}

		entries = append(entries, entry{
			p: &system.Process{
				Pid:  pid,
				User: user,
				Cmd:  cmd,
				Cpu:  cpuPct,
				Mem:  float64(memPct),
			},
			cpu: cpuPct,
		})
	}

	// advance baseline regardless of whether we could compute deltas
	ps.last = nextLast
	ps.lastAt = now

	// on the first call there is no baseline, so no deltas could be computed
	if elapsed <= 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].cpu > entries[j].cpu
	})

	if len(entries) > topProcessCount {
		entries = entries[:topProcessCount]
	}

	out := make([]*system.Process, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.p)
	}
	return out
}
