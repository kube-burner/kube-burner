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
	"os"
	"path/filepath"
	"testing"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTarball(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tarball Suite")
}

type mockIndexer struct {
	indexed map[string][]any
}

func newMockIndexer() *mockIndexer {
	return &mockIndexer{indexed: make(map[string][]any)}
}

func (m *mockIndexer) Index(docs []interface{}, opts indexers.IndexingOpts) (string, error) {
	m.indexed[opts.MetricName] = docs
	return "indexed", nil
}

func writeJSONFile(dir, name string, data any) {
	b, err := json.Marshal(data)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, name), b, 0o644)).To(Succeed())
}

var _ = Describe("CreateTarball", func() {
	It("creates tarball from flat directory", func() {
		tmpDir := GinkgoT().TempDir()
		metricsDir := filepath.Join(tmpDir, "metrics")
		tarballPath := filepath.Join(tmpDir, "out.tar.gz")

		writeJSONFile(metricsDir, "cpu.json", []map[string]any{{"value": 42}})
		writeJSONFile(metricsDir, "mem.json", []map[string]any{{"value": 99}})

		err := CreateTarball(indexers.IndexerConfig{
			MetricsDirectory: metricsDir,
			TarballName:      tarballPath,
		})
		Expect(err).NotTo(HaveOccurred())

		info, err := os.Stat(tarballPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(BeNumerically(">", 0))
	})

	It("creates tarball from nested directories", func() {
		tmpDir := GinkgoT().TempDir()
		metricsDir := filepath.Join(tmpDir, "metrics")
		nestedDir := filepath.Join(metricsDir, "sub", "deep")
		tarballPath := filepath.Join(tmpDir, "out.tar.gz")

		writeJSONFile(metricsDir, "top.json", []map[string]any{{"a": 1}})
		writeJSONFile(nestedDir, "nested.json", []map[string]any{{"b": 2}})

		err := CreateTarball(indexers.IndexerConfig{
			MetricsDirectory: metricsDir,
			TarballName:      tarballPath,
		})
		Expect(err).NotTo(HaveOccurred())

		info, err := os.Stat(tarballPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(BeNumerically(">", 0))
	})

	It("returns error for missing metrics directory", func() {
		tmpDir := GinkgoT().TempDir()
		err := CreateTarball(indexers.IndexerConfig{
			MetricsDirectory: filepath.Join(tmpDir, "nope"),
			TarballName:      filepath.Join(tmpDir, "out.tar.gz"),
		})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("CreateTarball and ImportTarball round-trip", func() {
	It("round-trips flat files", func() {
		tmpDir := GinkgoT().TempDir()
		metricsDir := filepath.Join(tmpDir, "metrics")
		tarballPath := filepath.Join(tmpDir, "out.tar.gz")

		writeJSONFile(metricsDir, "cpu.json", []map[string]any{{"cpu": 0.5}, {"cpu": 0.7}})
		writeJSONFile(metricsDir, "mem.json", []map[string]any{{"mem": 1024}})

		Expect(CreateTarball(indexers.IndexerConfig{
			MetricsDirectory: metricsDir,
			TarballName:      tarballPath,
		})).To(Succeed())

		mock := newMockIndexer()
		var idx indexers.Indexer = mock
		Expect(ImportTarball(tarballPath, &idx)).To(Succeed())

		Expect(mock.indexed).To(HaveLen(2))
		Expect(mock.indexed).To(HaveKey("cpu"))
		Expect(mock.indexed).To(HaveKey("mem"))
		Expect(mock.indexed["cpu"]).To(HaveLen(2))
		Expect(mock.indexed["mem"]).To(HaveLen(1))
	})

	It("round-trips nested files", func() {
		tmpDir := GinkgoT().TempDir()
		metricsDir := filepath.Join(tmpDir, "metrics")
		nestedDir := filepath.Join(metricsDir, "subdir")
		tarballPath := filepath.Join(tmpDir, "out.tar.gz")

		writeJSONFile(metricsDir, "top.json", []map[string]any{{"x": 1}})
		writeJSONFile(nestedDir, "deep.json", []map[string]any{{"y": 2}})

		Expect(CreateTarball(indexers.IndexerConfig{
			MetricsDirectory: metricsDir,
			TarballName:      tarballPath,
		})).To(Succeed())

		mock := newMockIndexer()
		var idx indexers.Indexer = mock
		var err = ImportTarball(tarballPath, &idx)
		Expect(err).To(Succeed())
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.indexed).To(HaveLen(2))
		Expect(mock.indexed).To(HaveKey("top"))
		Expect(mock.indexed).To(HaveKey("deep"))
	})
})

var _ = Describe("ImportTarball", func() {
	It("returns error for nonexistent file", func() {
		mock := newMockIndexer()
		var idx indexers.Indexer = mock
		err := ImportTarball("/nonexistent/path.tar.gz", &idx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not open tarball file"))
	})
})
