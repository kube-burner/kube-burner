package measurements

import (
	"context"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"

	"k8s.io/apimachinery/pkg/labels"
)

type pprof struct {
	config      config.Measurement
	stopChannel chan struct{}
}

func init() {
	measurementMap["pprof"] = &pprof{}
}

func (p *pprof) setConfig(cfg config.Measurement) {
	p.config = cfg
}

func (p *pprof) start() {
	err := os.MkdirAll("pprof", 0744)
	if err != nil {
		log.Fatalf("Error creating pprof directory: %s", err)
	}
	ticker := time.NewTicker(p.config.PProfInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				p.getPProf()
			case <-p.stopChannel:
			}
		}
	}()
}

func getPods(target config.PProftarget) []corev1.Pod {
	labelSelector := labels.Set(target.LabelSelector).String()
	podList, err := factory.clientSet.CoreV1().Pods(target.Namespace).List(context.TODO(), v1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error found listing pods labeled with %s: %s", labelSelector, err)
	}
	return podList.Items
}

func (p *pprof) getPProf() {
	var wg sync.WaitGroup
	for _, target := range p.config.PProfTargets {
		log.Infof("Collecting %s pprof", target.Name)
		podList := getPods(target)
		for _, pod := range podList {
			wg.Add(1)
			go func(target config.PProftarget, pod corev1.Pod) {
				defer wg.Done()
				pprofFile := fmt.Sprintf("%s-%s-%d.pprof", target.Name, pod.Name, time.Now().Unix())
				f, err := os.Create(path.Join("pprof", pprofFile))
				if err != nil {
					log.Errorf("Error creating pprof file %s: %s", pprofFile, err)
					return
				}
				defer f.Close()
				containerName := pod.Spec.Containers[0].Name
				req := factory.clientSet.CoreV1().
					RESTClient().
					Post().
					Resource("pods").
					Name(pod.Name).
					Namespace(pod.Namespace).
					SubResource("exec")
				req.VersionedParams(&corev1.PodExecOptions{
					Command:   []string{"curl", "-sSLkH", fmt.Sprintf("Authorization:  Bearer %s", target.BearerToken), target.URL},
					Container: containerName,
					Stdin:     false,
					Stderr:    true,
					Stdout:    true,
				}, scheme.ParameterCodec)
				exec, err := remotecommand.NewSPDYExecutor(factory.restConfig, "POST", req.URL())
				if err != nil {
					log.Errorf("Failed to execute pprof command on %s: %s", target.Name, err)
				}
				err = exec.Stream(remotecommand.StreamOptions{
					Stdin:  nil,
					Stdout: f,
					Stderr: f,
				})
				if err != nil {
					log.Errorf("Failed to get results from %s: %s", target.Name, err)
				}
			}(target, pod)
		}
	}
	wg.Wait()
}

func (p *pprof) stop() error {
	return nil
}
