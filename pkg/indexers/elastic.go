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
	"crypto/tls"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"

	elasticsearch "github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esutil"
)

const elastic = "elastic"

// Elastic ElasticSearch instance
type Elastic struct {
	client *elasticsearch.Client
}

func init() {
	indexerMap[elastic] = &Elastic{}
}

func (esIndexer *Elastic) new(esConfig config.IndexerConfig) {
	cfg := elasticsearch.Config{
		Addresses: esConfig.ESServers,
		Username:  esConfig.Username,
		Password:  esConfig.Password,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: esConfig.InsecureSkipVerify}},
	}
	ESClient, err := elasticsearch.NewClient(cfg)
	if err != nil {
		log.Errorf("Error creating the ES client: %s", err)
	}
	r, err := ESClient.Ping()
	if err != nil {
		log.Fatalf("ES connection error: %s", err)
	}
	if r.StatusCode != 200 {
		log.Fatalf("Unexpected ES status code: %d", r.StatusCode)
	}
	esIndexer.client = ESClient
}

// Index uses bulkIndexer to index the documents in the given index
func (esIndexer *Elastic) Index(index string, documents []interface{}) {
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:     esIndexer.client,
		Index:      index,
		FlushBytes: 5e+6,
		NumWorkers: runtime.NumCPU(),
	})
	if err != nil {
		log.Errorf("Error creating the indexer: %s", err)
	}
	r, _ := esIndexer.client.Indices.Exists([]string{index})
	if r.IsError() {
		log.Infof("Creating index %s", index)
		r, _ = esIndexer.client.Indices.Create(index)
		if r.IsError() {
			log.Warnf("Error creating index %s on ES: %s", index, r.String())
		}
	}
	start := time.Now().UTC()
	for _, document := range documents {
		j, err := json.Marshal(document)
		if err != nil {
			log.Errorf("Cannot encode document %s: %s", document, err)
		}
		err = bi.Add(
			context.Background(),
			esutil.BulkIndexerItem{
				Action: "index",
				Body:   bytes.NewReader(j),
			},
		)
		if err != nil {
			log.Errorf("Unexpected ES indexing error: %s", err)
		}
	}
	if err := bi.Close(context.Background()); err != nil {
		log.Fatalf("Unexpected ES error: %s", err)
	}
	stats := bi.Stats()
	dur := time.Since(start)
	log.Infof("Successfully indexed [%d] documents in %s milliseconds in %s", stats.NumFlushed, dur.Truncate(time.Millisecond), index)
}
