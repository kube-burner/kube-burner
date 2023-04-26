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

	opensearch "github.com/opensearch-project/opensearch-go"
	opensearchutil "github.com/opensearch-project/opensearch-go/opensearchutil"
)

const indexer = "opensearch"

// OpenSearch OpenSearch instance
type OpenSearch struct {
	client *opensearch.Client
	index  string
}

// Init function
func init() {
	indexerMap[indexer] = &OpenSearch{}
}

// Returns new indexer for OpenSearch
func (OpenSearchIndexer *OpenSearch) new(indexerConfig IndexerConfig) error {
	OpenSearchConfig := indexerConfig
	if OpenSearchConfig.Index == "" {
		return fmt.Errorf("index name not specified")
	}
	OpenSearchIndex := strings.ToLower(OpenSearchConfig.Index)
	cfg := opensearch.Config{
		Addresses: OpenSearchConfig.Servers,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: OpenSearchConfig.InsecureSkipVerify}},
	}
	OpenSearchClient, err := opensearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("error creating the OpenSearch client: %s", err)
	}
	r, err := OpenSearchClient.Cluster.Health()
	if err != nil {
		return fmt.Errorf("OpenSearch health check failed: %s", err)
	}
	if r.StatusCode != 200 {
		return fmt.Errorf("unexpected OpenSearch status code: %d", r.StatusCode)
	}
	OpenSearchIndexer.client = OpenSearchClient
	OpenSearchIndexer.index = OpenSearchIndex
	r, _ = OpenSearchIndexer.client.Indices.Exists([]string{OpenSearchIndex})
	if r.IsError() {
		r, _ = OpenSearchIndexer.client.Indices.Create(OpenSearchIndex)
		if r.IsError() {
			return fmt.Errorf("error creating index %s on OpenSearch: %s", OpenSearchIndex, r.String())
		}
	}
	return nil
}

// Index uses bulkIndexer to index the documents in the given index
func (OpenSearchIndexer *OpenSearch) Index(documents []interface{}, opts IndexingOpts) (string, error) {
	var statString string
	var indexerStatsLock sync.Mutex
	indexerStats := make(map[string]int)
	hasher := sha256.New()
	bi, err := opensearchutil.NewBulkIndexer(opensearchutil.BulkIndexerConfig{
		Client:     OpenSearchIndexer.client,
		Index:      OpenSearchIndexer.index,
		FlushBytes: 5e+6,
		NumWorkers: runtime.NumCPU(),
		Timeout:    10 * time.Minute, // TODO: hardcoded
	})
	if err != nil {
		return "", fmt.Errorf("Error creating the indexer: %s", err)
	}
	start := time.Now().UTC()
	for _, document := range documents {
		j, err := json.Marshal(document)
		if err != nil {
			return "", fmt.Errorf("Cannot encode document %s: %s", document, err)
		}
		hasher.Write(j)
		err = bi.Add(
			context.Background(),
			opensearchutil.BulkIndexerItem{
				Action:     "index",
				Body:       bytes.NewReader(j),
				DocumentID: hex.EncodeToString(hasher.Sum(nil)),
				OnSuccess: func(c context.Context, bii opensearchutil.BulkIndexerItem, biri opensearchutil.BulkIndexerResponseItem) {
					indexerStatsLock.Lock()
					defer indexerStatsLock.Unlock()
					indexerStats[biri.Result]++
				},
			},
		)
		if err != nil {
			return "", fmt.Errorf("Unexpected OpenSearch indexing error: %s", err)
		}
		hasher.Reset()
	}
	if err := bi.Close(context.Background()); err != nil {
		return "", fmt.Errorf("Unexpected OpenSearch error: %s", err)
	}
	dur := time.Since(start)
	for stat, val := range indexerStats {
		statString += fmt.Sprintf(" %s=%d", stat, val)
	}
	return fmt.Sprintf("Indexing finished in %v:%v", dur.Truncate(time.Millisecond), statString), nil
}
