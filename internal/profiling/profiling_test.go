package profiling

import (
	"fmt"
	"sync"
	"testing"
)

func BenchmarkTimer(b *testing.B) {
	for routines := 1; routines <= 1<<8; routines <<= 1 {
		b.Run(fmt.Sprintf("goroutines:%d", routines), func(b *testing.B) {
			stat := NewStat("foo")
			perRoutine := b.N / routines
			var wg sync.WaitGroup
			for r := 0; r < routines; r++ {
				wg.Add(1)
				go func() {
					for i := 0; i < perRoutine; i++ {
						timer := stat.NewTimer("bar")
						timer.Egress()
					}
					wg.Done()
				}()
			}
			wg.Wait()
		})
	}
}
