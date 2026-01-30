package util

import (
	"bytes"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PodPortForwarder struct {
	PodName  string
	StopChan chan struct{}
}

func NewPodPortForwarder(clientset kubernetes.Interface, restConfig rest.Config, port, namespace, podName string) (*PodPortForwarder, error) {
	roundTripper, upgrader, err := spdy.RoundTripperFor(&restConfig)
	if err != nil {
		return &PodPortForwarder{}, err
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, req.URL())

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{}, 1)
	errorChan := make(chan error, 1)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	forwarder, err := portforward.New(dialer, []string{port}, stopChan, readyChan, out, errOut)
	if err != nil {
		return &PodPortForwarder{}, err
	}

	go func() {
		// Locks until stopChan is closed.
		if err = forwarder.ForwardPorts(); err != nil {
			errorChan <- err
		}
	}()

	// Wait for the port-forwarding to be ready
	select {
	case <-readyChan:
		if len(errOut.String()) != 0 {
			close(stopChan)
			return &PodPortForwarder{}, fmt.Errorf("port forwarding error output: %s", errOut.String())
		}
		log.Infof("Port forwarding started between %s:%s", port, port)
	case err := <-errorChan:
		log.Errorf("Error during port-forwarding: %v", err)
		close(stopChan)
		return &PodPortForwarder{}, err
	}

	return &PodPortForwarder{
		PodName:  podName,
		StopChan: stopChan,
	}, nil
}

func (ppf *PodPortForwarder) CancelPodPortForwarder() {
	close(ppf.StopChan)
}
