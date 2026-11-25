// Copyright 2026 The Kube-burner Authors.
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

package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/cloud-bulldozer/go-commons/v2/podfiles"
	log "github.com/sirupsen/logrus"
)

// ResultsConfig is the run context stamped onto results produced by a tool
// kube-burner drove but does not own.
type ResultsConfig struct {
	// UUID of the run. Required: without it the documents cannot be
	// correlated with anything else and there is no point indexing them.
	UUID string
	// MetricName the documents are indexed under. Required.
	MetricName string
	// JobName the results belong to.
	JobName string
	// Metadata is the usual metadata block, nested under "metadata" exactly
	// as Prometheus metrics do.
	Metadata map[string]any
	// Extra carries context the tool itself did not record, for example
	// which target a result came from.
	Extra map[string]any
	// Timestamp stamped on every document. Zero means now.
	Timestamp time.Time
}

// ResultDocuments converts files collected from a pod into indexable
// kube-burner documents. Each file may hold a single JSON object or an array
// of them.
func ResultDocuments(files []podfiles.File, cfg ResultsConfig) ([]any, error) {
	if cfg.UUID == "" || cfg.MetricName == "" {
		return nil, fmt.Errorf("a UUID and a MetricName are required to index results")
	}
	if cfg.Timestamp.IsZero() {
		cfg.Timestamp = time.Now().UTC()
	}

	var documents []any
	for _, file := range files {
		records, err := decodeRecords(file.Content)
		if err != nil {
			log.Warnf("Skipping %s: %v", file.Path, err)
			continue
		}
		for _, record := range records {
			documents = append(documents, stamp(record, file.Path, cfg))
		}
	}
	log.Infof("Converted %d documents from %d result files", len(documents), len(files))
	return documents, nil
}

// IndexResults ships converted documents through an indexer.
func IndexResults(documents []any, metricName string, indexer indexers.Indexer) error {
	if len(documents) == 0 {
		return nil
	}
	resp, err := indexer.Index(documents, indexers.IndexingOpts{MetricName: metricName})
	if err != nil {
		return err
	}
	log.Info(resp)
	return nil
}

// decodeRecords accepts a single JSON object or an array of them.
func decodeRecords(raw []byte) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty file")
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []map[string]any
		err := json.Unmarshal([]byte(trimmed), &list)
		return list, err
	}
	var single map[string]any
	if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
		return nil, err
	}
	return []map[string]any{single}, nil
}

// stamp normalises a record's field names and adds the run's identity.
func stamp(record map[string]any, sourceFile string, cfg ResultsConfig) map[string]any {
	fields, _ := normalize(record).(map[string]any)

	for key, value := range cfg.Extra {
		fields[key] = value
	}
	fields["uuid"] = cfg.UUID
	fields["metricName"] = cfg.MetricName
	if cfg.JobName != "" {
		fields["jobName"] = cfg.JobName
	}
	if cfg.Metadata != nil {
		fields["metadata"] = cfg.Metadata
	}
	fields["timestamp"] = cfg.Timestamp.UTC().Format(time.RFC3339)
	fields["sourceFile"] = sourceFile
	return fields
}

// normalize recursively camelCases keys and drops nulls, so an external
// tool's output reads like the rest of kube-burner's. A null percentile means
// "not measured"; indexing it as zero would read as a real measurement.
func normalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if nested == nil {
				continue
			}
			clean := normalize(nested)
			if inner, ok := clean.(map[string]any); ok && len(inner) == 0 {
				continue
			}
			out[camelCase(key)] = clean
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				out = append(out, normalize(item))
			}
		}
		return out
	default:
		return value
	}
}

func camelCase(key string) string {
	isSeparator := func(r rune) bool { return r == '_' || r == '-' || r == ' ' || r == '.' }
	parts := strings.FieldsFunc(key, isSeparator)
	if len(parts) == 0 {
		return key
	}
	if len(parts) == 1 {
		return parts[0]
	}
	var out strings.Builder
	for i, part := range parts {
		if i == 0 {
			out.WriteString(strings.ToLower(part))
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		out.WriteString(string(runes))
	}
	return out.String()
}
