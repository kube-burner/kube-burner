// Copyright 2024 The Kube-burner Authors.
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

package alerting

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/model"
)

func TestAlerting(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alerting Suite")
}

var _ = Describe("parseMatrix", func() {
	var (
		now    time.Time
		before time.Time
		after  time.Time
		matrix model.Matrix
	)

	BeforeEach(func() {
		now = time.Now().UTC()
		before = now.Add(-10 * time.Minute)
		after = now.Add(10 * time.Minute)

		sampleTimestamp := model.TimeFromUnix(now.Unix())
		matrix = model.Matrix{
			&model.SampleStream{
				Metric: model.Metric{"__name__": "test_metric"},
				Values: []model.SamplePair{
					{Timestamp: sampleTimestamp, Value: 1.0},
				},
			},
		}
	})

	Context("when both churnStart and churnEnd are nil", func() {
		It("should not tag the alert as a churn metric", func() {
			result, err := parseMatrix(matrix, "test-uuid", "test alert fired", nil, sevWarn, nil, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))

			a, ok := result[0].(alert)
			Expect(ok).To(BeTrue())
			Expect(a.ChurnMetric).To(BeFalse())
		})
	})

	Context("when churnStart is set but churnEnd is nil", func() {
		It("should not panic and should not tag as churn metric", func() {
			result, err := parseMatrix(matrix, "test-uuid", "test alert fired", nil, sevWarn, &before, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))

			a, ok := result[0].(alert)
			Expect(ok).To(BeTrue())
			Expect(a.ChurnMetric).To(BeFalse())
		})
	})

	Context("when both are set and alert falls inside the churn window", func() {
		It("should tag the alert as a churn metric", func() {
			result, err := parseMatrix(matrix, "test-uuid", "test alert fired", nil, sevWarn, &before, &after)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))

			a, ok := result[0].(alert)
			Expect(ok).To(BeTrue())
			Expect(a.ChurnMetric).To(BeTrue())
		})
	})

	Context("when both are set but alert falls outside the churn window", func() {
		It("should not tag the alert as a churn metric", func() {
			result, err := parseMatrix(matrix, "test-uuid", "test alert fired", nil, sevWarn, &after, &after)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))

			a, ok := result[0].(alert)
			Expect(ok).To(BeTrue())
			Expect(a.ChurnMetric).To(BeFalse())
		})
	})
})
