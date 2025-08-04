package metrics

import (
	"testing"
	"time"

	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/stretchr/testify/assert"
)

func TestCheckThreshold_Exceeds(t *testing.T) {
	thresholds := []types.LatencyThreshold{
		{
			Metric:        "P99",
			ConditionType: "write",
			Threshold:     50 * time.Millisecond,
		},
	}
	quantiles := []any{
		LatencyQuantiles{
			QuantileName: "write",
			P99:          100,
		},
	}
	err := CheckThreshold(thresholds, quantiles)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "latency (0.10s) higher than configured threshold")
}

func TestCheckThreshold_UnknownMetric(t *testing.T) {
	thresholds := []types.LatencyThreshold{
		{
			Metric:        "P99",
			ConditionType: "nonexistent",
			Threshold:     50 * time.Millisecond,
		},
	}
	quantiles := []any{
		LatencyQuantiles{
			QuantileName: "write",
			P99:          100,
		},
	}
	err := CheckThreshold(thresholds, quantiles)
	assert.NoError(t, err)
}

func TestNewLatencySummary(t *testing.T) {
	input := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	name := "test-latency"

	quantiles := NewLatencySummary(input, name)

	t.Logf("Computed Quantiles: P50=%v, P95=%v, P99=%v, Min=%v, Max=%v, Avg=%v",
		quantiles.P50, quantiles.P95, quantiles.P99, quantiles.Min, quantiles.Max, quantiles.Avg)

	assert.Equal(t, "test-latency", quantiles.QuantileName)

	assert.Equal(t, 50, quantiles.P50)
	assert.Equal(t, 95, quantiles.P95)
	assert.Equal(t, 95, quantiles.P99)

	assert.Equal(t, 10, quantiles.Min)
	assert.Equal(t, 100, quantiles.Max)
	assert.Equal(t, 55, quantiles.Avg)
	assert.WithinDuration(t, time.Now().UTC(), quantiles.Timestamp, time.Second)
}
