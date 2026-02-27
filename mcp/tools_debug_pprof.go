//go:build pprof

package mcp

import (
	"bytes"
	"fmt"
	"runtime/pprof"
	"strings"
)

func pprofProcessGoroutine(pid string) (any, error) {
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return nil, fmt.Errorf("goroutine profile not available")
	}

	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, 2); err != nil {
		return nil, fmt.Errorf("failed to write goroutine profile: %w", err)
	}

	dump := buf.String()
	goroutines := strings.Split(dump, "\n\n")

	pidLabel := fmt.Sprintf("\"pid\":\"%s\"", pid)

	var matched []string
	for _, g := range goroutines {
		if strings.Contains(g, pidLabel) {
			matched = append(matched, strings.TrimSpace(g))
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no goroutine found with pid %s (pprof IS enabled). The process is likely in Sleep state -- sleeping processes park their goroutine so it does not appear in the dump. Use process_state tool to verify. If the process is Running or WaitResponse, the PID may be incorrect", pid)
	}

	return textResult(strings.Join(matched, "\n\n")), nil
}
