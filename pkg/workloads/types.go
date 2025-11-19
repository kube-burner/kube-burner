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
	"time"

	ocpmetadata "github.com/cloud-bulldozer/go-commons/v2/ocp-metadata"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
)

type Config struct {
	UUID            string
	Timeout         time.Duration
	MetricsEndpoint string
	UserMetadata    string
	PrometheusURL   string
	PrometheusToken string
}

type WorkloadHelper struct {
	Config
	kubeClientProvider *config.KubeClientProvider
	MetadataAgent      ocpmetadata.Metadata
	SummaryMetadata    map[string]interface{}
	MetricsMetadata    map[string]interface{}
	embedCfg           *fileutils.EmbedConfiguration
}
