package system

// PingResult is the latency-test outcome for a single ping target, reported by
// the agent. It is carried in CombinedData and served via the realtime channel;
// it is not persisted to history.
type PingResult struct {
	// Id is the target identifier assigned by the hub.
	Id string `json:"id" cbor:"0,keyasint"`
	// Latency is the most recent successful round-trip in milliseconds (0 if the
	// last probe failed).
	Latency float64 `json:"l" cbor:"1,keyasint"`
	// Loss is the packet-loss percentage over the recent sample window.
	Loss float64 `json:"lo" cbor:"2,keyasint"`
	// Avg is the average latency (ms) over successful probes in the window.
	Avg float64 `json:"a" cbor:"3,keyasint"`
}
