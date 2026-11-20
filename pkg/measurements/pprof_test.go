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

package measurements

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestGetPProfExecutorError(t *testing.T) {
	podList := corev1.PodList{
		Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "target-pod", Namespace: "default", Labels: map[string]string{"app": "test"}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(podList)
	}))
	defer srv.Close()
	clientSet, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := &pprof{BaseMeasurement: BaseMeasurement{
		Config: types.Measurement{
			PProfDirectory: dir,
			PProfTargets: []types.PProftarget{{
				Name:          "test",
				Namespace:     "default",
				LabelSelector: map[string]string{"app": "test"},
				URL:           "http://localhost:8082/debug/pprof/profile",
			}},
		},
		JobConfig: &config.Job{Name: "job"},
		ClientSet: clientSet,
		// A CA file with the insecure flag makes remotecommand.NewSPDYExecutor fail
		RestConfig: &rest.Config{Host: srv.URL, TLSClientConfig: rest.TLSClientConfig{Insecure: true, CAFile: filepath.Join(dir, "ca.crt")}},
	}}
	pods, err := p.getPods(p.Config.PProfTargets[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod from the fake API server, got %d", len(pods))
	}
	// Distinguish the executor-creation failure from the stream failure by its log line
	hook := logrustest.NewGlobal()
	defer hook.Reset()
	p.getPProf("end")
	matches, err := filepath.Glob(filepath.Join(dir, "job", "test", "*.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected no leftover pprof files, found %v", matches)
	}
	executorErrLogged := false
	for _, entry := range hook.Entries {
		if strings.Contains(entry.Message, "Failed to execute pprof command") {
			executorErrLogged = true
		}
	}
	if !executorErrLogged {
		t.Error("expected the NewSPDYExecutor error branch to run, but its log line was not emitted")
	}
}
