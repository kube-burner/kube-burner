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

	"github.com/vishnuchalla/perfscale-go-commons/logger"
)

var indexerMap = make(map[IndexerType]Indexer)

// NewIndexer creates a new Indexer with the specified IndexerConfig
func NewIndexer(indexerConfig IndexerConfig) (*Indexer, error) {
	var indexer Indexer
	var exists bool
	cfg := indexerConfig
	if indexer, exists = indexerMap[cfg.Type]; exists {
		logger.Infof("📁 Creating indexer: %s", cfg.Type)
		err := indexer.new(indexerConfig)
		if err != nil {
			return &indexer, err
		}
	} else {
		return &indexer, fmt.Errorf("Indexer not found: %s", cfg.Type)
	}
	return &indexer, nil
}
