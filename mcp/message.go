package mcp

import (
	"encoding/json"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/net/edf"
)

func init() {
	types := []any{
		ToolCallRequest{},
		ToolCallResponse{},
	}
	for _, t := range types {
		err := edf.RegisterTypeOf(t)
		if err == nil || err == gen.ErrTaken {
			continue
		}
		panic(err)
	}
}

// ToolCallRequest is sent via Call for inter-node tool dispatch.
// Entry point worker -> remote Pool -> remote worker HandleCall.
// Uses string for Params to ensure EDF serializability (json.RawMessage is []byte, not supported).
type ToolCallRequest struct {
	Tool   string
	Params string
}

// ToolCallResponse is returned from remote worker HandleCall.
type ToolCallResponse struct {
	Result string
	Error  string
}

// SampleReadRequest is sent to a sampler to read collected entries.
type SampleReadRequest struct {
	Since int
}

// SampleReadResponse contains sampler data collected since the given sequence.
type SampleReadResponse struct {
	SamplerID string        `json:"sampler_id"`
	Mode      string        `json:"mode"`
	Tool      string        `json:"tool,omitempty"`
	Sequence  int           `json:"sequence"`
	Completed bool          `json:"completed"`
	Samples   []SampleEntry `json:"samples"`
}

// SampleEntry is a single sampled data point.
type SampleEntry struct {
	Sequence  int       `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// helper to convert json.RawMessage to string for ToolCallRequest
func rawToString(raw json.RawMessage) string {
	if raw == nil {
		return "{}"
	}
	return string(raw)
}

// helper to convert string back to json.RawMessage
func stringToRaw(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}
