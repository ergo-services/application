package observer

import "ergo.services/ergo/app/system/inspect"

type wireMCPGetHeapProfile struct {
	Records      []wireMCPHeapRecord
	TotalInuse   int64 `unit:"bytes"`
	TotalAlloc   int64 `unit:"bytes"`
	TotalObjects int64
	Truncated    int
	Error        string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var wireMCPHeapProfileLegend = mcpLegendFor(wireMCPGetHeapProfile{})

func init() {
	mcpRegisterView(inspect.ResponseGetHeapProfile{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetHeapProfile)
		if ok == false {
			return value
		}
		return wireMCPGetHeapProfile{
			Records: wireMCPHeapRecords(r.Records), TotalInuse: r.TotalInuse,
			TotalAlloc:   r.TotalAlloc,
			TotalObjects: r.TotalObjects, Truncated: r.Truncated,
			Error:  mcpErrorText(r.Error),
			Legend: wireMCPHeapProfileLegend,
		}
	})
}
