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

package prometheus

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/prometheus/common/model"
)

func TestPrometheus(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Prometheus Suite")
}

var _ = Describe("createMetric groupId tagging", func() {
	var (
		p          Prometheus
		now        time.Time
		groupStart time.Time
		groupEnd   time.Time
		labels     model.Metric
	)

	BeforeEach(func() {
		p = Prometheus{
			UUID:     "test-uuid",
			metadata: map[string]any{},
		}
		now = time.Now().UTC()
		groupStart = now.Add(-10 * time.Minute)
		groupEnd = now.Add(10 * time.Minute)
		labels = model.Metric{"__name__": "test_metric"}
	})

	Context("when the job has no group windows", func() {
		It("should not tag the datapoint with a groupId", func() {
			job := Job{JobConfig: config.Job{Name: "job-1"}}
			m := p.createMetric("query", "test_metric", job, labels, 1.0, now, false)
			meta, ok := m.Metadata.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(meta).ToNot(HaveKey("groupId"))
		})
	})

	Context("when the datapoint falls within a group window", func() {
		It("should tag the datapoint with the matching groupId", func() {
			windows := []config.GroupWindow{{ID: 2, Start: groupStart, End: groupEnd}}
			job := Job{JobConfig: config.Job{Name: "job-1", GroupWindows: &windows}}
			m := p.createMetric("query", "test_metric", job, labels, 1.0, now, false)
			meta, ok := m.Metadata.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(meta["groupId"]).To(Equal(2))
		})
	})

	Context("when the datapoint falls outside all group windows", func() {
		It("should not tag the datapoint with a groupId", func() {
			windows := []config.GroupWindow{{ID: 1, Start: groupStart, End: groupEnd}}
			job := Job{JobConfig: config.Job{Name: "job-1", GroupWindows: &windows}}
			m := p.createMetric("query", "test_metric", job, labels, 1.0, now.Add(20*time.Minute), false)
			meta, ok := m.Metadata.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(meta).ToNot(HaveKey("groupId"))
		})
	})

	Context("with multiple group windows", func() {
		It("should tag each datapoint with the groupId of the window it falls into", func() {
			g1Start := now.Add(-30 * time.Minute)
			g1End := now.Add(-20 * time.Minute)
			g2Start := now.Add(-10 * time.Minute)
			g2End := now.Add(10 * time.Minute)
			windows := []config.GroupWindow{
				{ID: 1, Start: g1Start, End: g1End},
				{ID: 2, Start: g2Start, End: g2End},
			}
			job := Job{JobConfig: config.Job{Name: "job-1", GroupWindows: &windows}}

			m1 := p.createMetric("query", "test_metric", job, labels, 1.0, now.Add(-25*time.Minute), false)
			meta1 := m1.Metadata.(map[string]any)
			Expect(meta1["groupId"]).To(Equal(1))

			m2 := p.createMetric("query", "test_metric", job, labels, 1.0, now, false)
			meta2 := m2.Metadata.(map[string]any)
			Expect(meta2["groupId"]).To(Equal(2))
		})
	})

	Context("when the query is instant", func() {
		It("should not tag the datapoint with a groupId", func() {
			windows := []config.GroupWindow{{ID: 2, Start: groupStart, End: groupEnd}}
			job := Job{JobConfig: config.Job{Name: "job-1", GroupWindows: &windows}}
			m := p.createMetric("query", "test_metric", job, labels, 1.0, now, true)
			meta, ok := m.Metadata.(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(meta).ToNot(HaveKey("groupId"))
		})
	})
})
