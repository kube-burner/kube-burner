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
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"

	"k8s.io/apimachinery/pkg/labels"
)

type pprof struct {
	directory   string
	config      types.Measurement
	stopChannel chan bool
}

func init() {
	measurementMap["pprof"] = &pprof{}
}

func (p *pprof) setConfig(cfg types.Measurement) error {
	p.directory = "pprof"
	if cfg.PProfDirectory != "" {
		p.directory = cfg.PProfDirectory
	}
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

func (p *pprof) start() {
	var wg sync.WaitGroup
	err := os.MkdirAll(p.directory, 0744)
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
}

func getPods(target types.PProftarget) []corev1.Pod {
	labelSelector := labels.Set(target.LabelSelector).String()
	podList, err := factory.clientSet.CoreV1().Pods(target.Namespace).List(context.TODO(), v1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error found listing pods labeled with %s: %s", labelSelector, err)
	}
	return podList.Items
}

func (p *pprof) getPProf(wg *sync.WaitGroup, copyCerts bool) {
	var command []string
	for _, target := range p.config.PProfTargets {
		if target.BearerToken != "" && target.Cert != "" {
			log.Errorf("bearerToken and cert auth methods cannot be specified together, skipping pprof target")
			continue
		}
		log.Infof("Collecting %s pprof", target.Name)
		podList := getPods(target)
		for _, pod := range podList {
			wg.Add(1)
			go func(target types.PProftarget, pod corev1.Pod) {
				defer wg.Done()
				pprofFile := fmt.Sprintf("%s-%s-%d.pprof", target.Name, pod.Name, time.Now().Unix())
				f, err := os.Create(path.Join(p.directory, pprofFile))
				var stderr bytes.Buffer
				if err != nil {
					log.Errorf("Error creating pprof file %s: %s", pprofFile, err)
					return
				}
				defer f.Close()
				if target.Cert != "" && target.Key != "" && copyCerts {
					cert, privKey, err := readCerts(target.Cert, target.Key)
					if err != nil {
						log.Error(err)
						return
					}
					defer cert.Close()
					defer privKey.Close()
					if err != nil {
						log.Error(err)
						return
					}
					if err = copyCertsToPod(pod, cert, privKey); err != nil {
						log.Error(err)
						return
					}
				}
				if target.BearerToken != "" {
					command = []string{"curl", "-sSLkH", fmt.Sprintf("Authorization:  Bearer %s", target.BearerToken), target.URL}
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
				err = exec.Stream(remotecommand.StreamOptions{
					Stdin:  nil,
					Stdout: f,
					Stderr: &stderr,
				})
				if err != nil {
					log.Errorf("Failed to get pprof from %s: %s", pod.Name, stderr.String())
				}
			}(target, pod)
		}
	}
	wg.Wait()
}

func (p *pprof) stop() (int, error) {
	p.stopChannel <- true
	return 0, nil
}

func readCerts(cert, privKey string) (*os.File, *os.File, error) {
	var certFd, privKeyFd *os.File
	certFd, err := os.Open(cert)
	if err != nil {
		return certFd, privKeyFd, fmt.Errorf("Cannot read %s, skipping: %v", cert, err)
	}
	privKeyFd, err = os.Open(privKey)
	if err != nil {
		return certFd, privKeyFd, fmt.Errorf("Cannot read %s, skipping: %v", cert, err)
	}
	return certFd, privKeyFd, nil
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
		err = exec.Stream(remotecommand.StreamOptions{
			Stdin:  f,
			Stdout: nil,
			Stderr: &stderr,
		})
		if err != nil {
			return fmt.Errorf("Failed to copy file to %s: %s", pod.Name, stderr.Bytes())
		}
	}
	log.Infof("Certificate and private key copied into %s %s", pod.Name, pod.Spec.Containers[0].Name)
	return nil
}

func (p *pprof) validateConfig() error {
	return nil
}
