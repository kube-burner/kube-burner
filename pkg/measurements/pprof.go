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

package measurements

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"

	"k8s.io/apimachinery/pkg/labels"
)

type pprof struct {
	BaseMeasurement

	stopChannel chan bool
}

type pprofLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newPprofLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	for _, target := range measurement.PProfTargets {
		if target.BearerToken != "" && (target.CertFile != "" || target.Cert != "") {
			return nil, fmt.Errorf("bearerToken and cert auth methods cannot be specified together in the same target")
		}
	}

	return pprofLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (plmf pprofLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &pprof{
		BaseMeasurement: plmf.NewBaseLatency(jobConfig, clientSet, restConfig, "", "", embedCfg),
	}
}

func (p *pprof) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	var wg sync.WaitGroup
	err := os.MkdirAll(p.Config.PProfDirectory, 0744)
	if err != nil {
		log.Fatalf("Error creating pprof directory: %s", err)
	}
	if p.needsDaemonSet() {
		if err := p.deployDaemonSet(); err != nil {
			log.Errorf("Error deploying DaemonSet: %s", err)
			return err
		}
		if err := p.waitForDaemonSetReady(); err != nil {
			log.Errorf("Error waiting for DaemonSet to be ready: %s", err)
			return err
		}
	}
	p.stopChannel = make(chan bool)
	p.getPProf(&wg, true)
	wg.Wait()
	go func() {
		defer close(p.stopChannel)
		ticker := time.NewTicker(p.Config.PProfInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Copy certificates only in the first iteration
				p.getPProf(&wg, false)
				wg.Wait()
			case <-p.stopChannel:
				ticker.Stop()
				return
			}
		}
	}()
	return nil
}

func (p *pprof) getPods(target types.PProftarget) []corev1.Pod {
	// If DaemonSet is deployed and no explicit label selector is provided, use DaemonSet pods
	if p.getPprofNodeTargets(target) != nil { // Node target
		labelSelector := labels.Set(p.getPprofNodeTargets(target)).String()
		podList, err := p.ClientSet.CoreV1().Pods(types.PprofNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector,
			FieldSelector: "status.phase=Running",
		})
		if err != nil {
			log.Errorf("Error found listing DaemonSet pods: %s", err)
			return []corev1.Pod{}
		}
		return podList.Items
	}
	labelSelector := labels.Set(target.LabelSelector).String()
	podList, err := p.ClientSet.CoreV1().Pods(target.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error found listing pods labeled with %s: %s", labelSelector, err)
	}
	return podList.Items
}

func (p *pprof) getPProf(wg *sync.WaitGroup, first bool) {
	var err error
	for pos, target := range p.Config.PProfTargets {
		log.Infof("Collecting %s pprof", target.Name)
		podList := p.getPods(target)
		for _, pod := range podList {
			var cert, privKey io.Reader
			if target.CertFile != "" && target.KeyFile != "" && first {
				// target is a copy of one of the slice elements, so we need to modify the target object directly from the slice
				p.Config.PProfTargets[pos].Cert, p.Config.PProfTargets[pos].Key, err = readCerts(target.CertFile, target.KeyFile)
				if err != nil {
					log.Error(err)
					continue
				}
			}
			if target.Cert != "" && target.Key != "" && first {
				certData, err := base64.StdEncoding.DecodeString(target.Cert)
				if err != nil {
					log.Errorf("Error decoding pprof certificate data from %s", target.Name)
					continue
				}
				privKeyData, err := base64.StdEncoding.DecodeString(target.Key)
				if err != nil {
					log.Errorf("Error decoding pprof private key data from %s", target.Name)
					continue
				}
				cert = strings.NewReader(string(certData))
				privKey = strings.NewReader(string(privKeyData))
			}
			wg.Add(1)
			go func(target types.PProftarget, pod corev1.Pod) {
				defer wg.Done()

				// Determine identifier for filename
				identifier := pod.Name

				pprofFile := fmt.Sprintf("%s-%s-%d.pprof", target.Name, identifier, time.Now().Unix())
				f, err := os.Create(path.Join(p.Config.PProfDirectory, pprofFile))
				var stderr bytes.Buffer
				if err != nil {
					log.Errorf("Error creating pprof file %s: %s", pprofFile, err)
					return
				}
				defer f.Close()
				if cert != nil && privKey != nil && first {
					if err = p.copyCertsToPod(pod, cert, privKey); err != nil {
						log.Error(err)
						return
					}
				}

				command, pprofReq := p.buildPProfRequest(target, pod)

				log.Tracef("Collecting pprof using URL: %s", pprofReq.URL())
				pprofReq.VersionedParams(&corev1.PodExecOptions{
					Command:   command,
					Container: pod.Spec.Containers[0].Name,
					Stdin:     false,
					Stderr:    true,
					Stdout:    true,
				}, scheme.ParameterCodec)
				log.Debugf("Executing %s in pod %s (namespace: %s)", command, pod.Name, pod.Namespace)
				exec, err := remotecommand.NewSPDYExecutor(p.RestConfig, "POST", pprofReq.URL())
				if err != nil {
					log.Errorf("Failed to execute pprof command on %s: %s", target.Name, err)
				}
				err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
					Stdin:  nil,
					Stdout: f,
					Stderr: &stderr,
				})
				if err != nil {
					log.Errorf("Failed to get pprof from %s: %s", pod.Name, stderr.String())
					os.Remove(f.Name())
				} else {
					log.Debugf("Successfully collected pprof data: %s", pprofFile)
				}
			}(p.Config.PProfTargets[pos], pod)
		}
	}
	wg.Wait()
}

func (p *pprof) buildPProfRequest(target types.PProftarget, pod corev1.Pod) ([]string, *rest.Request) {
	var pprofReq *rest.Request
	var command []string

	// Build command based on target type
	if p.getPprofNodeTargets(target) != nil {
		// Node-level collection via DaemonSet
		if strings.HasPrefix(target.URL, "unix://") {
			// Unix socket (CRI-O)
			socketPath := strings.TrimPrefix(target.URL, "unix://")

			// Extract the pprof path from target.URL or use default
			pprofPath := "/debug/pprof/profile"
			if strings.Contains(target.Name, "heap") {
				pprofPath = "/debug/pprof/heap"
			} else if strings.Contains(target.Name, "goroutine") {
				pprofPath = "/debug/pprof/goroutine"
			}

			// Convert duration to seconds for CPU profiling
			seconds := int(p.Config.PProfInterval.Seconds())
			if seconds > 300 {
				seconds = 30 // Cap at 30 seconds for CPU profiling
			}

			command = []string{"curl", "-fsSLk",
				"--unix-socket", socketPath,
				fmt.Sprintf("http://localhost%s?seconds=%d", pprofPath, seconds),
			}
			log.Debugf("Using unix socket: %s for %s", socketPath, pprofPath)
		} else {
			// HTTPS endpoint (kubelet) - use insecure since kubelet typically uses self-signed certs
			command = []string{"sh", "-c",
				fmt.Sprintf("curl -fsSLk -H \"Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)\" %s", target.URL),
			}
		}
		pprofReq = p.ClientSet.CoreV1().
			RESTClient().
			Post().
			Resource("pods").
			Name(pod.Name).
			Namespace(types.PprofNamespace).
			SubResource("exec")
	} else {
		// Pod-level collection (etcd, api-server, etc.)
		if target.BearerToken != "" {
			command = []string{"curl", "-fsSLkH", fmt.Sprintf("Authorization: Bearer %s", target.BearerToken), target.URL}
		} else if target.Cert != "" && target.Key != "" {
			command = []string{"curl", "-fsSLk", "--cert", "/tmp/pprof.crt", "--key", "/tmp/pprof.key", target.URL}
		} else {
			command = []string{"curl", "-fsSLk", target.URL}
		}
		pprofReq = p.ClientSet.CoreV1().
			RESTClient().
			Post().
			Resource("pods").
			Name(pod.Name).
			Namespace(pod.Namespace).
			SubResource("exec")
	}
	return command, pprofReq
}

func (p *pprof) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

func (p *pprof) Stop() error {
	p.stopChannel <- true
	return nil
}

// Fake index function for pprof
func (p *pprof) Index(_ string, _ map[string]indexers.Indexer) {
}

func readCerts(cert, privKey string) (string, string, error) {
	var certFd, privKeyFd *os.File
	var certData, privKeyData []byte
	certFd, err := os.Open(cert)
	if err != nil {
		return "", "", fmt.Errorf("cannot read %s, skipping: %v", cert, err)
	}
	privKeyFd, err = os.Open(privKey)
	if err != nil {
		return "", "", fmt.Errorf("cannot read %s, skipping: %v", cert, err)
	}
	certData, err = io.ReadAll(certFd)
	if err != nil {
		return "", "", err
	}
	privKeyData, err = io.ReadAll(privKeyFd)
	if err != nil {
		return "", "", err
	}
	return string(certData), string(privKeyData), nil
}

func (p *pprof) copyCertsToPod(pod corev1.Pod, cert, privKey io.Reader) error {
	var stderr bytes.Buffer
	log.Infof("Copying certificate and private key into %s %s", pod.Name, pod.Spec.Containers[0].Name)
	fMap := map[string]io.Reader{
		"/tmp/pprof.crt": cert,
		"/tmp/pprof.key": privKey,
	}
	for dest, f := range fMap {
		req := p.ClientSet.CoreV1().
			RESTClient().
			Post().
			Resource("pods").
			Name(pod.Name).
			Namespace(pod.Namespace).
			SubResource("exec")
		req.VersionedParams(&corev1.PodExecOptions{
			Command:   []string{"tee", dest},
			Container: pod.Spec.Containers[0].Name,
			Stdin:     true,
			Stderr:    true,
			Stdout:    false,
		}, scheme.ParameterCodec)
		exec, err := remotecommand.NewSPDYExecutor(p.RestConfig, "POST", req.URL())
		if err != nil {
			return fmt.Errorf("failed to establish SPDYExecutor on %s: %s", pod.Name, err)
		}
		err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
			Stdin:  f,
			Stdout: nil,
			Stderr: &stderr,
		})
		if err != nil || stderr.String() != "" {
			return fmt.Errorf("failed to copy file to %s: %s", pod.Name, stderr.Bytes())
		}
	}
	log.Infof("Certificate and private key copied into %s %s", pod.Name, pod.Spec.Containers[0].Name)
	return nil
}

func (p *pprof) IsCompatible() bool {
	return true
}
