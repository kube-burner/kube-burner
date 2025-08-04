package watchers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
	"k8s.io/client-go/rest"
)

func TestWatcherManagerStartWithInvalidKind(t *testing.T) {
	wm := NewWatcherManager(&rest.Config{}, rate.NewLimiter(rate.Inf, 1))

	wm.Start("InvalidKind", "v1", map[string]string{}, 0, 0)

	errs := wm.Wait()

	assert.NotEmpty(t, errs, "Expected error due to invalid kind")
	assert.Contains(t, errs[0].Error(), "error getting GVR")
}

func TestWatcherManagerStopAllWithNoWatchers(t *testing.T) {
	wm := NewWatcherManager(&rest.Config{}, rate.NewLimiter(rate.Inf, 1))

	errs := wm.StopAll()
	assert.Empty(t, errs, "Expected no errors when stopping zero watchers")
}

func TestWatcherManagerRecordError(t *testing.T) {
	wm := NewWatcherManager(&rest.Config{}, rate.NewLimiter(rate.Inf, 1))

	testErr := errors.New("test error")
	wm.recordError(testErr)

	wm.errMu.Lock()
	defer wm.errMu.Unlock()

	assert.Len(t, wm.errs, 1, "Expected one recorded error")
	assert.EqualError(t, wm.errs[0], "test error")
}
