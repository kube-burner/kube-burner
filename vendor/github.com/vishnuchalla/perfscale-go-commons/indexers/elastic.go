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
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/vishnuchalla/perfscale-go-commons/logger"

	elasticsearch "github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esutil"
)

const elastic = "elastic"

// Elastic ElasticSearch instance
type Elastic struct {
	client *elasticsearch.Client
	index  string
}

func init() {
	indexerMap[elastic] = &Elastic{}
}

func (esIndexer *Elastic) new(indexerConfig IndexerConfig) error {
	esConfig := indexerConfig
	if esConfig.DefaultIndex == "" {
		return fmt.Errorf("index name not specified")
	}
	esIndex := strings.ToLower(esConfig.DefaultIndex)
	cfg := elasticsearch.Config{
		Addresses: esConfig.ESServers,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: esConfig.InsecureSkipVerify}},
	}
	ESClient, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("error creating the ES client: %s", err)
	}
	r, err := ESClient.Cluster.Health()
	if err != nil {
		return fmt.Errorf("ES health check failed: %s", err)
	}
	if r.StatusCode != 200 {
		return fmt.Errorf("unexpected ES status code: %d", r.StatusCode)
	}
	esIndexer.client = ESClient
	esIndexer.index = esIndex
	r, _ = esIndexer.client.Indices.Exists([]string{esIndex})
	if r.IsError() {
		logger.Infof("Creating index %s", esIndex)
		r, _ = esIndexer.client.Indices.Create(esIndex)
		if r.IsError() {
			return fmt.Errorf("error creating index %s on ES: %s", esIndex, r.String())
		}
	}
	return nil
}

// Index uses bulkIndexer to index the documents in the given index
func (esIndexer *Elastic) Index(documents []interface{}, opts IndexingOpts) {
	var statString string
	var indexerStatsLock sync.Mutex
	logger.Infof("Indexing metric %s", opts.MetricName)
	indexerStats := make(map[string]int)
	hasher := sha256.New()
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:     esIndexer.client,
		Index:      esIndexer.index,
		FlushBytes: 5e+6,
		NumWorkers: runtime.NumCPU(),
		Timeout:    10 * time.Minute, // TODO: hardcoded
	})
	if err != nil {
		logger.Errorf("Error creating the indexer: %s", err)
	}
	start := time.Now().UTC()
	logger.Debugf("Indexing [%d] documents in %s", len(documents), esIndexer.index)
	for _, document := range documents {
		j, err := json.Marshal(document)
		if err != nil {
			logger.Errorf("Cannot encode document %s: %s", document, err)
		}
		hasher.Write(j)
		err = bi.Add(
			context.Background(),
			esutil.BulkIndexerItem{
				Action:     "index",
				Body:       bytes.NewReader(j),
				DocumentID: hex.EncodeToString(hasher.Sum(nil)),
				OnSuccess: func(c context.Context, bii esutil.BulkIndexerItem, biri esutil.BulkIndexerResponseItem) {
					indexerStatsLock.Lock()
					defer indexerStatsLock.Unlock()
					indexerStats[biri.Result]++
				},
			},
		)
		if err != nil {
			logger.Errorf("Unexpected ES indexing error: %s", err)
		}
		hasher.Reset()
	}
	if err := bi.Close(context.Background()); err != nil {
		logger.Fatalf("Unexpected ES error: %s", err)
	}
	dur := time.Since(start)
	for stat, val := range indexerStats {
		statString += fmt.Sprintf(" %s=%d", stat, val)
	}
	logger.Debugf("Indexing finished in %v:%v", dur.Truncate(time.Millisecond), statString)
}
