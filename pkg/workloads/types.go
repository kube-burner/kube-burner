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

package workloads

import (
	"embed"
	"time"

	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
)

type CustomMeasurement struct {
	Func measurements.Measurement
	Name string
}

type Config struct {
	UUID               string
	Timeout            time.Duration
	MetricsEndpoint    string
	UserMetadata       string
	ConfigDir          string
	PrometheusURL      string
	PrometheusToken    string
	CustomMeasurements []CustomMeasurement
}

type WorkloadHelper struct {
	Config
	embedConfig        embed.FS
	kubeClientProvider *config.KubeClientProvider
	MetadataAgent      ocpmetadata.Metadata
	SummaryMetadata    map[string]interface{}
	MetricsMetadata    map[string]interface{}
}
