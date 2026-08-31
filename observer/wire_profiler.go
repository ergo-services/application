package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireGoroutineGroup struct {
	Count   int    `json:"c"`
	State   string `json:"s,omitempty"`
	WaitSec int64  `json:"w,omitempty"`
	Origin  string `json:"o,omitempty"`
	Current string `json:"cu,omitempty"`
	Stack   string `json:"st,omitempty"`
	IDs     []int  `json:"i,omitempty"`
}

type wireGoroutines struct {
	Groups   []wireGoroutineGroup `json:"g"`
	Total    int                  `json:"t,omitempty"`
	Filtered int                  `json:"f,omitempty"`
}

func wireGoroutinesFrom(r inspect.ResponseGetGoroutines) wireGoroutines {
	out := wireGoroutines{
		Groups:   make([]wireGoroutineGroup, 0, len(r.Groups)),
		Total:    r.Total,
		Filtered: r.Filtered,
	}
	for _, group := range r.Groups {
		out.Groups = append(out.Groups, wireGoroutineGroup{
			Count:   group.Count,
			State:   group.State,
			WaitSec: group.WaitSec,
			Origin:  group.Origin,
			Current: group.Current,
			Stack:   group.Stack,
			IDs:     group.IDs,
		})
	}
	return out
}

type wireHeapRecord struct {
	InuseBytes   int64    `json:"ib,omitempty"`
	InuseObjects int64    `json:"io,omitempty"`
	AllocBytes   int64    `json:"ab,omitempty"`
	AllocObjects int64    `json:"ao,omitempty"`
	FreeObjects  int64    `json:"fo,omitempty"`
	Stack        []string `json:"s,omitempty"`
}

type wireHeapProfile struct {
	Records      []wireHeapRecord `json:"r"`
	TotalInuse   int64            `json:"ti,omitempty"`
	TotalAlloc   int64            `json:"ta,omitempty"`
	TotalObjects int64            `json:"to,omitempty"`
}

func wireHeapProfileFrom(r inspect.ResponseGetHeapProfile) wireHeapProfile {
	out := wireHeapProfile{
		Records:      make([]wireHeapRecord, 0, len(r.Records)),
		TotalInuse:   r.TotalInuse,
		TotalAlloc:   r.TotalAlloc,
		TotalObjects: r.TotalObjects,
	}
	for _, record := range r.Records {
		out.Records = append(out.Records, wireHeapRecord{
			InuseBytes:   record.InuseBytes,
			InuseObjects: record.InuseObjects,
			AllocBytes:   record.AllocBytes,
			AllocObjects: record.AllocObjects,
			FreeObjects:  record.FreeObjects,
			Stack:        record.Stack,
		})
	}
	return out
}

type wireTypeStats struct {
	Enabled      bool  `json:"e,omitempty"`
	Encoded      int64 `json:"en,omitempty"`
	Decoded      int64 `json:"de,omitempty"`
	EncodedBytes int64 `json:"eb,omitempty"`
	DecodedBytes int64 `json:"db,omitempty"`
}

type wireRegisteredType struct {
	ID           uint64        `json:"i"`
	Name         string        `json:"n"`
	Kind         string        `json:"k,omitempty"`
	Schema       string        `json:"sc,omitempty"`
	Proto        string        `json:"p,omitempty"`
	MinSize      uint32        `json:"ms,omitempty"`
	SizeVariable bool          `json:"sv,omitempty"`
	Stats        wireTypeStats `json:"st,omitempty"`
}

type wireTypes struct {
	Types []wireRegisteredType `json:"t"`
}

func wireTypesFrom(r inspect.ResponseGetTypes) wireTypes {
	out := wireTypes{Types: make([]wireRegisteredType, 0, len(r.Types))}
	for _, info := range r.Types {
		out.Types = append(out.Types, wireRegisteredTypeFrom(info))
	}
	return out
}

func wireRegisteredTypeFrom(info gen.RegisteredTypeInfo) wireRegisteredType {
	return wireRegisteredType{
		ID:           info.ID,
		Name:         info.Name,
		Kind:         info.Kind,
		Schema:       info.Schema,
		Proto:        info.Proto,
		MinSize:      info.MinSize,
		SizeVariable: info.SizeVariable,
		Stats: wireTypeStats{
			Enabled:      info.Stats.Enabled,
			Encoded:      info.Stats.Encoded,
			Decoded:      info.Stats.Decoded,
			EncodedBytes: info.Stats.EncodedBytes,
			DecodedBytes: info.Stats.DecodedBytes,
		},
	}
}
