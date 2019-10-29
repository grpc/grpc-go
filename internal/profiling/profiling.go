package profiling

import (
	"sync/atomic"
	"time"
	"runtime"
)

var profilingEnabled uint32

func IsEnabled() bool {
	return atomic.LoadUint32(&profilingEnabled) > 0
}

func SetEnabled(enabled bool) {
	if enabled {
		atomic.StoreUint32(&profilingEnabled, 1)
	} else {
		atomic.StoreUint32(&profilingEnabled, 0)
	}
}

type Timer struct {
	TimerTag string
	Begin time.Time
	End time.Time
	GoId int64
}

func NewTimer(timerTag string) *Timer {
	timer := &Timer{TimerTag: timerTag, GoId: runtime.GoId()}
	timer.Ingress()
	return timer
}

func (t *Timer) Ingress() {
	if t == nil {
		return
	}

	t.Begin = time.Now()
}

func (t *Timer) Egress() {
	if t == nil {
		return
	}

	t.End = time.Now()
}

type Stat struct {
	StatTag string
	Timers []*Timer
	Metadata []byte
}

func NewStat(statTag string) *Stat {
	return &Stat{StatTag: statTag, Timers: make([]*Timer, 0)}
}

func (stat *Stat) NewTimer(timerTag string) *Timer {
	if (stat == nil) {
		return nil
	}

	timer := NewTimer(timerTag)
	stat.Timers = append(stat.Timers, timer)
	return timer
}

func (stat *Stat) AppendTimer(timer *Timer) {
	if (stat == nil) {
		return
	}

	stat.Timers = append(stat.Timers, timer)
}

var StreamStats *CircularBuffer

func InitStats(bufsize uint32) (err error) {
	StreamStats, err = NewCircularBuffer(bufsize)
	if err != nil {
		return
	}

	return
}

var IdCounter uint64
