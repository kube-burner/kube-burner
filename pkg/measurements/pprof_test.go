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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestPProfTimeout(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		configured time.Duration
		want       time.Duration
	}{
		{"heap profile uses the default", "http://localhost:6060/debug/pprof/heap", 0, config.DefaultPProfTimeout},
		{"cpu profile adds the sampling window", "http://localhost:6060/debug/pprof/profile?seconds=30", 0, config.DefaultPProfTimeout + 30*time.Second},
		{"bogus seconds is ignored", "http://localhost:6060/debug/pprof/profile?seconds=abc", 0, config.DefaultPProfTimeout},
		{"explicit timeout wins", "http://localhost:6060/debug/pprof/profile?seconds=30", 10 * time.Second, 10 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pprofTimeout(types.PProftarget{URL: tt.url}, tt.configured)
			if got != tt.want {
				t.Fatalf("pprofTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPProfRequestSetsCurlMaxTime(t *testing.T) {
	clientSet, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	p := &pprof{BaseMeasurement: BaseMeasurement{ClientSet: clientSet, Config: types.Measurement{PProfTimeout: 90 * time.Second}}}
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"}}
	targets := []types.PProftarget{
		{URL: "http://svc:6060/debug/pprof/heap", LabelSelector: map[string]string{"app": "x"}},
		{URL: "http://svc:6060/debug/pprof/heap", LabelSelector: map[string]string{"app": "x"}, BearerToken: "token"},
		{URL: "http://svc:6060/debug/pprof/heap", LabelSelector: map[string]string{"app": "x"}, Cert: "c", Key: "k"},
	}
	for _, target := range targets {
		command, _, execTimeout := p.buildPProfRequest(target, pod)
		i := slices.Index(command, "--max-time")
		if i < 0 || i+1 >= len(command) || command[i+1] != "90" {
			t.Fatalf("expected --max-time 90 in %v", command)
		}
		if want := 90*time.Second + pprofExecGracePeriod; execTimeout != want {
			t.Fatalf("exec timeout = %v, want %v", execTimeout, want)
		}
	}
}

// An exec that never answers (e.g. curl stuck on an unreachable endpoint) used to block
// getPProf, and with it the measurement Stop, forever.
func TestGetPProfDoesNotHangOnStuckExec(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/ns/pods":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(corev1.PodList{
				TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"},
				Items: []corev1.Pod{{
					ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "ns"},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/exec"):
			// Accept the exec like the API server would, then never send any output,
			// which is what a curl stuck on an unreachable URL looks like from here.
			if _, err := httpstream.Handshake(r, w, []string{remotecommandconsts.StreamProtocolV4Name}); err != nil {
				return
			}
			conn := spdy.NewResponseUpgrader().UpgradeResponse(w, r, func(httpstream.Stream, <-chan struct{}) error { return nil })
			if conn == nil {
				return
			}
			defer conn.Close()
			<-release
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer close(release)

	oldGrace := pprofExecGracePeriod
	pprofExecGracePeriod = 0
	defer func() { pprofExecGracePeriod = oldGrace }()

	restConfig := &rest.Config{Host: server.URL}
	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := &pprof{BaseMeasurement: BaseMeasurement{
		ClientSet:  clientSet,
		RestConfig: restConfig,
		JobConfig:  &config.Job{Name: "job"},
		Config: types.Measurement{
			PProfDirectory: dir,
			PProfTimeout:   200 * time.Millisecond,
			PProfTargets: []types.PProftarget{{
				Name:          "stuck",
				Namespace:     "ns",
				LabelSelector: map[string]string{"app": "x"},
				URL:           "http://unreachable.svc.cluster.local:6060/debug/pprof/heap",
			}},
		},
	}}

	done := make(chan struct{})
	go func() {
		p.getPProf("end")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("getPProf didn't return after the pprof timeout")
	}
	if _, err := os.Stat(filepath.Join(dir, "job", "stuck", "target-end.pprof")); !os.IsNotExist(err) {
		t.Fatalf("expected the partial pprof file to be removed, stat err: %v", err)
	}
}
