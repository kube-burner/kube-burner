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

import (
	"fmt"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
)

// Indexer indexer interface
type Indexer interface {
	Index(string, []interface{})
	new(config.Spec) error
}

var indexerMap = make(map[string]Indexer)

// NewIndexer creates a new Indexer with the specified IndexerConfig
func NewIndexer(configSpec config.Spec) (*Indexer, error) {
	var indexer Indexer
	var exists bool
	cfg := configSpec.GlobalConfig.IndexerConfig
	if indexer, exists = indexerMap[cfg.Type]; exists {
		log.Infof("📁 Creating indexer: %s", cfg.Type)
		err := indexer.new(configSpec)
		if err != nil {
			return &indexer, err
		}
	} else {
		return &indexer, fmt.Errorf("Indexer not found: %s", cfg.Type)
	}
	return &indexer, nil
}
