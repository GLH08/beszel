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
// not sent over the wire to keep payloads small. 120 keeps payloads tiny
// while showing enough of the command (paths/flags/ports) to identify it.
const cmdDisplayLimit = 120

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
//
// To keep per-cycle I/O bounded on hosts with many processes, it first reads
// only CPU times for every pid (one /proc read each), computes the deltas,
// and only then reads memory/user/cmdline for the few top candidates.
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

	// cpuPct for every pid whose delta we can compute this round
	type delta struct {
		pid int32
		cpu float64
	}
	deltas := make([]delta, 0, len(procs))
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
		d := curTime - prev.cpuTime
		cpuPct := (d / elapsed.Seconds()) * 100 / float64(ps.cores)
		if cpuPct < 0 {
			cpuPct = 0
		}
		deltas = append(deltas, delta{pid: pid, cpu: cpuPct})
	}

	// advance baseline regardless of whether we could compute deltas
	ps.last = nextLast
	ps.lastAt = now

	// on the first call there is no baseline, so no deltas could be computed
	if elapsed <= 0 {
		return nil
	}

	// pick the top N by CPU% before doing the expensive per-process reads
	sort.Slice(deltas, func(i, j int) bool {
		return deltas[i].cpu > deltas[j].cpu
	})
	if len(deltas) > topProcessCount {
		deltas = deltas[:topProcessCount]
	}

	out := make([]*system.Process, 0, len(deltas))
	for _, d := range deltas {
		p, err := process.NewProcess(d.pid)
		if err != nil {
			continue
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
		out = append(out, &system.Process{
			Pid:  d.pid,
			User: user,
			Cmd:  cmd,
			Cpu:  d.cpu,
			Mem:  float64(memPct),
		})
	}

	// Diagnostic: log when the snapshot is unexpectedly empty despite having a
	// baseline, so container/PID-namespace issues are visible without debug.
	if len(out) == 0 && len(procs) > 0 {
		slog.Info("top processes empty", "procs", len(procs), "deltas", len(deltas), "elapsed_ms", elapsed.Milliseconds())
	}
	return out
}
