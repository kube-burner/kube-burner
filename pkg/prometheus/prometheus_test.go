package prometheus

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	testmock "github.com/stretchr/testify/mock"
)

type MockIndexer struct {
	testmock.Mock
}

func (m *MockIndexer) Index(data []any, opts indexers.IndexingOpts) (string, error) {
	args := m.Called(data, opts)
	return args.String(0), args.Error(1)
}

type MockPrometheusClient struct {
	testmock.Mock
}

func (m *MockPrometheusClient) Query(query string, ts time.Time) (model.Value, error) {
	args := m.Called(query, ts)
	return args.Get(0).(model.Value), args.Error(1)
}

func (m *MockPrometheusClient) QueryRange(query string, start, end time.Time, step time.Duration) (model.Value, error) {
	args := m.Called(query, start, end, step)
	return args.Get(0).(model.Value), args.Error(1)
}

func TestParseVector_InvalidType(t *testing.T) {
	p := &Prometheus{}
	job := Job{}
	var metrics []any
	val := model.Matrix{}

	err := p.parseVector("metric", "query", job, val, &metrics)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported result format")
}

func TestParseMatrix_InvalidType(t *testing.T) {
	p := &Prometheus{}
	job := Job{}
	var metrics []any
	val := model.Vector{}

	err := p.parseMatrix("metric", "query", job, val, &metrics)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported result format")
}

func TestCreateMetric_HandlesNaN(t *testing.T) {
	p := &Prometheus{UUID: "test-uuid", metadata: map[string]any{"source": "test"}}
	job := Job{}
	timestamp := time.Now().UTC()
	metric := p.createMetric("query", "metricName", job, model.Metric{}, model.SampleValue(math.NaN()), timestamp, true)

	assert.Equal(t, float64(0), metric.Value)
	assert.Equal(t, "test-uuid", metric.UUID)
	assert.Equal(t, timestamp, metric.Timestamp)
}

func TestIndexDatapoints_Success(t *testing.T) {
	mockIndexer := MockIndexer{}
	var indexer indexers.Indexer = &mockIndexer
	p := &Prometheus{indexer: &indexer}

	docs := map[string][]any{
		"cpu": {"doc1", "doc2"},
	}
	mockIndexer.On("Index", docs["cpu"], testmock.Anything).Return("success", nil)

	p.indexDatapoints(docs)
	mockIndexer.AssertExpectations(t)
}

func TestIndexDatapoints_Error(t *testing.T) {
	mockIndexer := MockIndexer{}
	var indexer indexers.Indexer = &mockIndexer
	p := &Prometheus{indexer: &indexer}

	docs := map[string][]any{
		"cpu": {"doc1"},
	}
	mockIndexer.On("Index", docs["cpu"], testmock.Anything).Return("", errors.New("index error"))

	p.indexDatapoints(docs)
	mockIndexer.AssertExpectations(t)
}
