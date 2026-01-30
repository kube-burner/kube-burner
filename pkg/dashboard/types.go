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
	"time"
)

// MessageType defines the type of WebSocket message
type MessageType string

const (
	// MessageTypeProgress indicates a job progress update
	MessageTypeProgress MessageType = "progress"
	// MessageTypeMeasurements indicates a measurement data update
	MessageTypeMeasurements MessageType = "measurements"
	// MessageTypeETA indicates an ETA calculation update
	MessageTypeETA MessageType = "eta"
	// MessageTypeComplete indicates job completion
	MessageTypeComplete MessageType = "complete"
	// MessageTypeSnapshot indicates a full state snapshot
	MessageTypeSnapshot MessageType = "snapshot"
)

// Job phase constants
const (
	// PhaseRunning indicates the job is actively running
	PhaseRunning = "running"
	// PhaseChurning indicates the job is in churning phase
	PhaseChurning = "churning"
	// PhaseVerifying indicates the job is verifying resources
	PhaseVerifying = "verifying"
	// PhaseComplete indicates the job has completed
	PhaseComplete = "complete"
)

// Message represents a WebSocket message sent to clients
type Message struct {
	Type      MessageType `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// JobProgress represents the progress state of a single job
type JobProgress struct {
	JobName          string   `json:"jobName"`
	CurrentIteration int      `json:"currentIteration"`
	TotalIterations  int      `json:"totalIterations"`
	ObjectsCreated   int32    `json:"objectsCreated"`
	PercentComplete  float64  `json:"percentComplete"`
	QPS              float64  `json:"qps"`
	Phase            string   `json:"phase"` // running|churning|verifying|complete
	Errors           []string `json:"errors,omitempty"`
}

// MeasurementSnapshot represents a snapshot of measurement data
type MeasurementSnapshot struct {
	MeasurementName string    `json:"measurementName"`
	MetricCount     int       `json:"metricCount"`
	LastUpdate      time.Time `json:"lastUpdate"`
}

// DashboardSnapshot represents the complete dashboard state
type DashboardSnapshot struct {
	Timestamp              time.Time             `json:"timestamp"`
	UUID                   string                `json:"uuid"`
	RunID                  string                `json:"runid"`
	ElapsedTime            string                `json:"elapsedTime"`
	EstimatedTimeRemaining string                `json:"estimatedTimeRemaining"`
	Jobs                   []JobProgress         `json:"jobs"`
	Measurements           []MeasurementSnapshot `json:"measurements"`
	OverallProgress        float64               `json:"overallProgress"`
	CurrentQPS             float64               `json:"currentQPS"`
	TotalObjectsCreated    int32                 `json:"totalObjectsCreated"`
}

// ETAUpdate represents an estimated time remaining update
type ETAUpdate struct {
	EstimatedTimeRemaining string  `json:"estimatedTimeRemaining"`
	OverallProgress        float64 `json:"overallProgress"`
}
