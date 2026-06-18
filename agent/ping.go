package agent

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/entities/system"
)

// pingInterval is how often each target is probed.
const pingInterval = 10 * time.Second

// pingTimeout is the per-probe deadline for a TCP dial.
const pingTimeout = 2 * time.Second

// pingSamplesKept is how many recent latency samples are kept per target for
// computing loss / average.
const pingSamplesKept = 12 // ~2 minutes of history at 10s interval

// pingResult is a single probe outcome for a target.
type pingResult struct {
	ok      bool
	latency float64 // milliseconds
}

// pingTargetState tracks recent probes for one target.
type pingTargetState struct {
	target  common.PingTarget
	samples []pingResult // ring of recent results
	mu      sync.Mutex
}

// pingManager probes a set of TCP targets at a fixed interval and holds the
// latest latency/loss summary per target. Targets are pushed from the hub via
// the SetConfig action.
type pingManager struct {
	mu      sync.RWMutex
	targets map[string]*pingTargetState // target id -> state

	// results snapshot read by gatherStats; replaced atomically on each cycle
	results map[string]*system.PingResult
	resultMu sync.RWMutex
}

func newPingManager() *pingManager {
	return &pingManager{
		targets: make(map[string]*pingTargetState),
		results: make(map[string]*system.PingResult),
	}
}

// SetTargets replaces the active target set. New targets start probing
// immediately; removed targets stop and are dropped from results.
func (pm *pingManager) SetTargets(targets []common.PingTarget) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// build the new set
	newSet := make(map[string]*pingTargetState, len(targets))
	for _, t := range targets {
		if st, ok := pm.targets[t.Id]; ok {
			st.target = t
			newSet[t.Id] = st
			continue
		}
		newSet[t.Id] = &pingTargetState{target: t}
	}
	pm.targets = newSet

	// prune results for removed targets
	pm.resultMu.Lock()
	for id := range pm.results {
		if _, ok := newSet[id]; !ok {
			delete(pm.results, id)
		}
	}
	pm.resultMu.Unlock()

	if len(newSet) > 0 {
		slog.Info("ping targets updated", "count", len(newSet))
	}
}

// Run starts the probe loop. It blocks until ctx is cancelled.
func (pm *pingManager) Run(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	// probe once immediately so results appear without waiting a full interval
	pm.probeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.probeAll(ctx)
		}
	}
}

func (pm *pingManager) probeAll(ctx context.Context) {
	pm.mu.RLock()
	states := make([]*pingTargetState, 0, len(pm.targets))
	for _, st := range pm.targets {
		states = append(states, st)
	}
	pm.mu.RUnlock()

	// probe concurrently so a slow target doesn't delay the others
	var wg sync.WaitGroup
	for _, st := range states {
		wg.Add(1)
		go func(st *pingTargetState) {
			defer wg.Done()
			res := probeTCP(ctx, st.target.Host, st.target.Port)
			st.mu.Lock()
			st.samples = append(st.samples, res)
			if len(st.samples) > pingSamplesKept {
				st.samples = st.samples[len(st.samples)-pingSamplesKept:]
			}
			st.mu.Unlock()
		}(st)
	}
	wg.Wait()

	// rebuild the results snapshot
	snapshot := make(map[string]*system.PingResult, len(states))
	for _, st := range states {
		st.mu.Lock()
		s := st.samples
		if len(s) == 0 {
			st.mu.Unlock()
			continue
		}
		loss := 0
		var total float64
		var last float64
		for i, r := range s {
			if !r.ok {
				loss++
				continue
			}
			total += r.latency
			if i == len(s)-1 {
				last = r.latency
			}
		}
		// "current" latency is the last probe's latency, or 0 if the last probe failed
		current := last
		if !s[len(s)-1].ok {
			current = 0
		}
		ok := len(s) - loss
		avg := 0.0
		if ok > 0 {
			avg = total / float64(ok)
		}
		lossPct := float64(loss) / float64(len(s)) * 100
		snapshot[st.target.Id] = &system.PingResult{
			Latency: current,
			Loss:    lossPct,
			Avg:     avg,
		}
		st.mu.Unlock()
	}

	pm.resultMu.Lock()
	pm.results = snapshot
	pm.resultMu.Unlock()
}

// Results returns a snapshot of current per-target latency/loss, keyed by
// target id. Returns nil if there are no targets or no results yet.
func (pm *pingManager) Results() map[string]*system.PingResult {
	pm.resultMu.RLock()
	defer pm.resultMu.RUnlock()
	if len(pm.results) == 0 {
		return nil
	}
	out := make(map[string]*system.PingResult, len(pm.results))
	for k, v := range pm.results {
		out[k] = v
	}
	return out
}

// probeTCP measures the round-trip time of a TCP handshake to host:port.
func probeTCP(ctx context.Context, host string, port uint16) pingResult {
	if host == "" || port == 0 {
		return pingResult{}
	}
	address := net.JoinHostPort(host, formatPort(port))
	dialer := net.Dialer{Timeout: pingTimeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", address)
	elapsed := time.Since(start)
	if err != nil {
		return pingResult{ok: false}
	}
	_ = conn.Close()
	return pingResult{ok: true, latency: float64(elapsed.Microseconds()) / 1000.0}
}

func formatPort(port uint16) string {
	// avoid strconv to keep the scratch binary minimal
	if port == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for port > 0 {
		i--
		buf[i] = byte('0' + port%10)
		port /= 10
	}
	return string(buf[i:])
}

// sortedResults returns results sorted by target id for stable wire output.
func (pm *pingManager) sortedResults() []*system.PingResult {
	res := pm.Results()
	if res == nil {
		return nil
	}
	ids := make([]string, 0, len(res))
	for id := range res {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*system.PingResult, 0, len(ids))
	for _, id := range ids {
		out = append(out, &system.PingResult{Id: id, Latency: res[id].Latency, Loss: res[id].Loss, Avg: res[id].Avg})
	}
	return out
}
