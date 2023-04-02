// Copyright 2020 The Kube-burner Authors.
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

package indexers

const (
	// Elastic indexer that send metrics to the configures ES instance
	ElasticIndexer IndexerType = "elastic"
	// Local indexer that writes metrics to local directory
	LocalIndexer IndexerType = "local"
)

// Indexer indexer interface
type Indexer interface {
	Index([]interface{}, IndexingOpts)
	new(IndexerConfig) error
}

// Indexing options
type IndexingOpts struct {
	MetricName string
	JobName    string
}

// IndexerType type of indexer
type IndexerType string

// IndexerConfig holds the indexer configuration
type IndexerConfig struct {
	// Type type of indexer
	Type IndexerType `yaml:"type"`
	// ESServers List of ElasticSearch instances
	ESServers []string `yaml:"esServers"`
	// DefaultIndex default index to send prometheus metrics
	DefaultIndex string `yaml:"defaultIndex"`
	// Port indexer port
	Port int `yaml:"port"`
	// InsecureSkipVerify disable TLS ceriticate verification
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
	// Enabled enable indexer
	Enabled bool `yaml:"enabled"`
	// Directory to save metrics files in
	MetricsDirectory string `yaml:"metricsDirectory"`
	// Create tarball
	CreateTarball bool `yaml:"createTarball"`
	// TarBall name
	TarballName string `yaml:"tarballName"`
}