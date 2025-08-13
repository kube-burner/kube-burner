package alerting

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockIndexer struct {
	mock.Mock
}

func (m *MockIndexer) Index(data []any, opts indexers.IndexingOpts) (string, error) {
	args := m.Called(data, opts)
	return args.String(0), args.Error(1)
}

func TestNewAlertManager_InvalidFile(t *testing.T) {
	am, err := NewAlertManager("non-existent.yaml", "uuid", &prometheus.Prometheus{}, nil, nil, nil)
	assert.Error(t, err)
	assert.NotNil(t, am)
}

func TestValidateTemplates(t *testing.T) {
	manager := AlertManager{
		alertProfile: alertProfile{
			{
				Description: "{{.Labels.app}} exceeded",
			},
		},
	}
	err := manager.validateTemplates()
	assert.NoError(t, err)
}

func TestParseMatrix_EmptyMatrix(t *testing.T) {
	matrix := model.Matrix{}
	alerts, err := parseMatrix(matrix, "uuid", "Alert fired", nil, sevError, nil, nil)
	assert.NoError(t, err)
	assert.Len(t, alerts, 0)
}

func TestParseMatrix_InvalidType(t *testing.T) {
	vec := model.Vector{}
	_, err := parseMatrix(vec, "uuid", "desc", nil, sevWarn, nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported result format")
}

func TestAlertManagerIndexing(t *testing.T) {
	mockIndexer := new(MockIndexer)
	var indexer indexers.Indexer = mockIndexer
	manager := AlertManager{
		indexer: &indexer,
	}
	data := []any{"alert1", "alert2"}
	mockIndexer.On("Index", data, mock.Anything).Return("ok", nil)

	manager.index(data)
	mockIndexer.AssertExpectations(t)
}

func TestAlertManagerIndexingError(t *testing.T) {
	mockIndexer := new(MockIndexer)
	var indexer indexers.Indexer = mockIndexer
	manager := AlertManager{
		indexer: &indexer,
	}
	data := []any{"alert1"}
	mockIndexer.On("Index", data, mock.Anything).Return("", errors.New("indexing failed"))

	manager.index(data)
	mockIndexer.AssertExpectations(t)
}

func TestValidateTemplates_BadTemplate(t *testing.T) {
	manager := AlertManager{
		alertProfile: alertProfile{
			{Description: "{{.Labels.app"},
		},
	}
	err := manager.validateTemplates()
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "template validation error"))
}
