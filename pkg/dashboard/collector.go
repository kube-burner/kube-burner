// Copyright 2024 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dashboard

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/measurements"
	log "github.com/sirupsen/logrus"
)

// No need for custom interfaces, we can use the actual measurements types

// Collector aggregates job progress and measurement data
type Collector struct {
	mu              sync.RWMutex
	jobProgress     map[string]*JobProgress
	measurements    map[string]*MeasurementSnapshot
	startTime       time.Time
	totalJobs       int
	completedJobs   int32
	uuid            string
	runID           string
	broadcaster     *Broadcaster
	totalObjectsOps int32
}

// NewCollector creates a new collector instance
func NewCollector(broadcaster *Broadcaster, totalJobs int) *Collector {
	return &Collector{
		jobProgress:   make(map[string]*JobProgress),
		measurements:  make(map[string]*MeasurementSnapshot),
		startTime:     time.Now(),
		totalJobs:     totalJobs,
		broadcaster:   broadcaster,
		completedJobs: 0,
	}
}

// SetRunInfo sets the UUID and RunID for this benchmark run
func (c *Collector) SetRunInfo(uuid, runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uuid = uuid
	c.runID = runID
}

// UpdateJobProgress updates the progress for a specific job and broadcasts the update
func (c *Collector) UpdateJobProgress(jobName string, progress JobProgress) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard collector panic recovered in UpdateJobProgress: %v", r)
		}
	}()

	c.mu.Lock()
	// Store or update job progress
	c.jobProgress[jobName] = &progress

	// Track completed jobs
	if progress.Phase == PhaseComplete {
		atomic.AddInt32(&c.completedJobs, 1)
	}

	// Update total object operations
	atomic.StoreInt32(&c.totalObjectsOps, progress.ObjectsCreated)
	c.mu.Unlock()

	// Broadcast progress update
	if c.broadcaster != nil {
		c.broadcaster.Send(Message{
			Type:      MessageTypeProgress,
			Timestamp: time.Now(),
			Data:      progress,
		})

		// Also send ETA update
		eta := c.CalculateETA()
		c.broadcaster.Send(Message{
			Type:      MessageTypeETA,
			Timestamp: time.Now(),
			Data: ETAUpdate{
				EstimatedTimeRemaining: formatDuration(eta),
				OverallProgress:        c.calculateOverallProgress(),
			},
		})
	}
}

// StartMeasurementPolling polls measurements at regular intervals and broadcasts updates
func (c *Collector) StartMeasurementPolling(ctx context.Context, measurementsInstance *measurements.Measurements, interval time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard collector panic recovered in StartMeasurementPolling: %v", r)
		}
	}()

	if measurementsInstance == nil {
		log.Debug("Dashboard: measurements is nil, skipping polling")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug("Dashboard measurement polling stopped")
			return
		case <-ticker.C:
			c.pollMeasurements(measurementsInstance)
		}
	}
}

// pollMeasurements polls current measurement data and broadcasts updates
func (c *Collector) pollMeasurements(measurementsInstance *measurements.Measurements) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard collector panic recovered in pollMeasurements: %v", r)
		}
	}()

	if measurementsInstance == nil || measurementsInstance.MeasurementsMap == nil {
		return
	}

	c.mu.Lock()
	snapshots := make(map[string]*MeasurementSnapshot)

	for name, measurement := range measurementsInstance.MeasurementsMap {
		if measurement == nil {
			continue
		}

		metrics := measurement.GetMetrics()
		if metrics == nil {
			continue
		}

		// Count metrics in sync.Map
		count := 0
		metrics.Range(func(_, _ interface{}) bool {
			count++
			return true
		})

		snapshots[name] = &MeasurementSnapshot{
			MeasurementName: name,
			MetricCount:     count,
			LastUpdate:      time.Now(),
		}
	}

	c.measurements = snapshots
	c.mu.Unlock()

	// Broadcast measurement update
	if c.broadcaster != nil && len(snapshots) > 0 {
		c.broadcaster.Send(Message{
			Type:      MessageTypeMeasurements,
			Timestamp: time.Now(),
			Data:      snapshots,
		})
	}
}

// CalculateETA calculates estimated time remaining based on current progress
func (c *Collector) CalculateETA() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.totalJobs == 0 {
		return 0
	}

	elapsedTime := time.Since(c.startTime)
	completedWork := float64(atomic.LoadInt32(&c.completedJobs))

	// Add fractional progress from currently running jobs
	for _, progress := range c.jobProgress {
		if progress.Phase == PhaseRunning || progress.Phase == PhaseChurning || progress.Phase == PhaseVerifying {
			completedWork += progress.PercentComplete / 100.0
		}
	}

	if completedWork == 0 {
		return 0 // Unknown ETA
	}

	totalWork := float64(c.totalJobs)
	estimatedTotalSeconds := elapsedTime.Seconds() * (totalWork / completedWork)
	remainingSeconds := estimatedTotalSeconds - elapsedTime.Seconds()

	if remainingSeconds < 0 {
		return 0
	}

	return time.Duration(remainingSeconds) * time.Second
}

// calculateOverallProgress calculates overall completion percentage
func (c *Collector) calculateOverallProgress() float64 {
	if c.totalJobs == 0 {
		return 0
	}

	completedWork := float64(atomic.LoadInt32(&c.completedJobs))

	// Add fractional progress from currently running jobs
	for _, progress := range c.jobProgress {
		if progress.Phase == PhaseRunning || progress.Phase == PhaseChurning || progress.Phase == PhaseVerifying {
			completedWork += progress.PercentComplete / 100.0
		}
	}

	return (completedWork / float64(c.totalJobs)) * 100.0
}

// GetSnapshot returns a complete snapshot of the current dashboard state
func (c *Collector) GetSnapshot() DashboardSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	jobs := make([]JobProgress, 0, len(c.jobProgress))
	for _, progress := range c.jobProgress {
		jobs = append(jobs, *progress)
	}

	measurementsList := make([]MeasurementSnapshot, 0, len(c.measurements))
	for _, snapshot := range c.measurements {
		measurementsList = append(measurementsList, *snapshot)
	}

	// Calculate current QPS
	var currentQPS float64
	for _, progress := range c.jobProgress {
		if progress.Phase == PhaseRunning {
			currentQPS += progress.QPS
		}
	}

	elapsedTime := time.Since(c.startTime)
	eta := c.CalculateETA()

	return DashboardSnapshot{
		Timestamp:              time.Now(),
		UUID:                   c.uuid,
		RunID:                  c.runID,
		ElapsedTime:            formatDuration(elapsedTime),
		EstimatedTimeRemaining: formatDuration(eta),
		Jobs:                   jobs,
		Measurements:           measurementsList,
		OverallProgress:        c.calculateOverallProgress(),
		CurrentQPS:             currentQPS,
		TotalObjectsCreated:    atomic.LoadInt32(&c.totalObjectsOps),
	}
}

// BroadcastSnapshot sends a complete snapshot to all clients
func (c *Collector) BroadcastSnapshot() {
	if c.broadcaster != nil {
		snapshot := c.GetSnapshot()
		c.broadcaster.Send(Message{
			Type:      MessageTypeSnapshot,
			Timestamp: time.Now(),
			Data:      snapshot,
		})
	}
}

// formatDuration formats a duration into a human-readable string
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "unknown"
	}

	d = d.Round(time.Second)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
