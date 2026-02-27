//go:build !pprof

package mcp

import "fmt"

func pprofProcessGoroutine(pid string) (any, error) {
	return nil, fmt.Errorf("looking up goroutine by process PID requires building with -tags=pprof. Without this tag, actor goroutines are not labeled with PID. Use pprof_goroutines without pid parameter to see all goroutines")
}
