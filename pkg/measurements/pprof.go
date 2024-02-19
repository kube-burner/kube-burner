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

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"

	"k8s.io/apimachinery/pkg/labels"
)

type pprof struct {
	config      types.Measurement
	stopChannel chan bool
}

func init() {
	measurementMap["pprof"] = &pprof{}
}

func (p *pprof) setConfig(cfg types.Measurement) {
	p.config = cfg
}

func (p *pprof) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	var wg sync.WaitGroup
	err := os.MkdirAll(p.config.PProfDirectory, 0744)
	if err != nil {
		log.Fatalf("Error creating pprof directory: %s", err)
	}
	p.stopChannel = make(chan bool)
	p.getPProf(&wg, true)
	wg.Wait()
	go func() {
		defer close(p.stopChannel)
		ticker := time.NewTicker(p.config.PProfInterval)
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

func getPods(target types.PProftarget) []corev1.Pod {
	labelSelector := labels.Set(target.LabelSelector).String()
	podList, err := factory.clientSet.CoreV1().Pods(target.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error found listing pods labeled with %s: %s", labelSelector, err)
	}
	return podList.Items
}

func (p *pprof) getPProf(wg *sync.WaitGroup, first bool) {
	var err error
	var command []string
	for pos, target := range p.config.PProfTargets {
		log.Infof("Collecting %s pprof", target.Name)
		podList := getPods(target)
		for _, pod := range podList {
			var cert, privKey io.Reader
			if target.CertFile != "" && target.KeyFile != "" && first {
				// target is a copy of one of the slice elements, so we need to modify the target object directly from the slice
				p.config.PProfTargets[pos].Cert, p.config.PProfTargets[pos].Key, err = readCerts(target.CertFile, target.KeyFile)
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
				pprofFile := fmt.Sprintf("%s-%s-%d.pprof", target.Name, pod.Name, time.Now().Unix())
				f, err := os.Create(path.Join(p.config.PProfDirectory, pprofFile))
				var stderr bytes.Buffer
				if err != nil {
					log.Errorf("Error creating pprof file %s: %s", pprofFile, err)
					return
				}
				defer f.Close()
				if cert != nil && privKey != nil && first {
					if err = copyCertsToPod(pod, cert, privKey); err != nil {
						log.Error(err)
						return
					}
				}
				if target.BearerToken != "" {
					command = []string{"curl", "-sSLkH", fmt.Sprintf("Authorization: Bearer %s", target.BearerToken), target.URL}
				} else if target.Cert != "" && target.Key != "" {
					command = []string{"curl", "-sSLk", "--cert", "/tmp/pprof.crt", "--key", "/tmp/pprof.key", target.URL}
				} else {
					command = []string{"curl", "-sSLk", target.URL}
				}
				req := factory.clientSet.CoreV1().
					RESTClient().
					Post().
					Resource("pods").
					Name(pod.Name).
					Namespace(pod.Namespace).
					SubResource("exec")
				log.Debugf("Collecting pprof using URL: %s", req.URL())
				req.VersionedParams(&corev1.PodExecOptions{
					Command:   command,
					Container: pod.Spec.Containers[0].Name,
					Stdin:     false,
					Stderr:    true,
					Stdout:    true,
				}, scheme.ParameterCodec)
				log.Debugf("Executing %s in pod %s", command, pod.Name)
				exec, err := remotecommand.NewSPDYExecutor(factory.restConfig, "POST", req.URL())
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
				}
			}(p.config.PProfTargets[pos], pod)
		}
	}
	wg.Wait()
}

func (p *pprof) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

func (p *pprof) stop() error {
	p.stopChannel <- true
	return nil
}

// Fake index function for pprof
func (p *pprof) index(_ indexers.Indexer, _ string) {
}

func readCerts(cert, privKey string) (string, string, error) {
	var certFd, privKeyFd *os.File
	var certData, privKeyData []byte
	certFd, err := os.Open(cert)
	if err != nil {
		return "", "", fmt.Errorf("Cannot read %s, skipping: %v", cert, err)
	}
	privKeyFd, err = os.Open(privKey)
	if err != nil {
		return "", "", fmt.Errorf("Cannot read %s, skipping: %v", cert, err)
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

func copyCertsToPod(pod corev1.Pod, cert, privKey io.Reader) error {
	var stderr bytes.Buffer
	log.Infof("Copying certificate and private key into %s %s", pod.Name, pod.Spec.Containers[0].Name)
	fMap := map[string]io.Reader{
		"/tmp/pprof.crt": cert,
		"/tmp/pprof.key": privKey,
	}
	for dest, f := range fMap {
		req := factory.clientSet.CoreV1().
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
		exec, err := remotecommand.NewSPDYExecutor(factory.restConfig, "POST", req.URL())
		if err != nil {
			return fmt.Errorf("Failed to establish SPDYExecutor on %s: %s", pod.Name, err)
		}
		err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
			Stdin:  f,
			Stdout: nil,
			Stderr: &stderr,
		})
		if err != nil || stderr.String() != "" {
			return fmt.Errorf("Failed to copy file to %s: %s", pod.Name, stderr.Bytes())
		}
	}
	log.Infof("Certificate and private key copied into %s %s", pod.Name, pod.Spec.Containers[0].Name)
	return nil
}

func (p *pprof) validateConfig() error {
	for _, target := range p.config.PProfTargets {
		if target.BearerToken != "" && (target.CertFile != "" || target.Cert != "") {
			return fmt.Errorf("bearerToken and cert auth methods cannot be specified together in the same target")
		}
	}
	return nil
}
