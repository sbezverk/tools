package stats_server

import (
	"sync/atomic"
	"time"
)

type StatsProvider interface {
	GetStatsJson() ([]byte, error)
}

func AtomicTimeGuard(timeStamp *atomic.Value) time.Time {
	if timeStamp == nil {
		return time.Time{}
	}
	if tm, ok := timeStamp.Load().(time.Time); ok {
		return tm
	}
	return time.Time{}
}
