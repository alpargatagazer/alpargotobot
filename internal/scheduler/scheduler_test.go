package scheduler

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScheduler(t *testing.T) {
	s, err := NewScheduler("UTC")
	assert.NoError(t, err)
	assert.NotNil(t, s)

	// Test invalid schedule_time format
	err = s.ScheduleDailyJob("invalid-time", func() {})
	assert.Error(t, err)

	// Test invalid timezone
	_, err = NewScheduler("Invalid/Timezone")
	assert.Error(t, err)

	// Test scheduling and execution using a fast trigger pattern if needed,
	// but mostly we test if AddFunc accepts the cron expression without error.
	var dailyCalled int32
	err = s.ScheduleDailyJob("12:34", func() {
		atomic.StoreInt32(&dailyCalled, 1)
	})
	assert.NoError(t, err)

	var purgeCalled int32
	err = s.SchedulePurgeJob(func() {
		atomic.StoreInt32(&purgeCalled, 1)
	})
	assert.NoError(t, err)

	s.Start()
	// Let it run for a microsecond and stop
	time.Sleep(10 * time.Millisecond)
	s.Stop()

	assert.Equal(t, int32(0), atomic.LoadInt32(&dailyCalled))
	assert.Equal(t, int32(0), atomic.LoadInt32(&purgeCalled))
}
