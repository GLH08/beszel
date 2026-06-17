package system

// Process is a single entry in the "top processes by CPU" snapshot.
// CBOR field numbers are part of the wire protocol — only append new ones.
type Process struct {
	Pid  int32   `json:"pid" cbor:"0,keyasint"`
	User string  `json:"u,omitempty" cbor:"1,keyasint,omitempty"`
	Cmd  string  `json:"c,omitempty" cbor:"2,keyasint,omitempty"`
	Cpu  float64 `json:"cpu" cbor:"3,keyasint"`
	Mem  float64 `json:"m" cbor:"4,keyasint"` // memory as percent of total RAM
}
