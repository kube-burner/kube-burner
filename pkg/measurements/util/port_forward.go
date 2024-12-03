package util

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PodPortForwarder struct {
	PodName   string
	LocalPort string
	StopChan  chan struct{}
}

// parsePort parses out the local port from the port-forward output string.
// Example: "Forwarding from 127.0.0.1:8000 -> 4000", returns "8000".
func parsePort(forwardAddr string) (string, error) {
	// Split the input into lines
	lines := strings.Split(forwardAddr, "\n")
	for _, line := range lines {
		// Remove any leading/trailing whitespace
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split the line into parts
		parts := strings.Split(line, " ")
		if len(parts) < 3 {
			continue
		}

		// Attempt to parse the local port
		_, localPort, err := net.SplitHostPort(parts[2])
		if err == nil {
			return localPort, nil
		}
	}

	return "", fmt.Errorf("unable to parse local port from stdout: %s", forwardAddr)
}

func NewPodPortForwarder(clientset kubernetes.Interface, restConfig rest.Config, remotePort, namespace, podName string) (PodPortForwarder, error) {
	var localPort string
	roundTripper, upgrader, err := spdy.RoundTripperFor(&restConfig)
	if err != nil {
		return PodPortForwarder{}, err
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
	ports := []string{fmt.Sprintf(":%s", remotePort)}
	forwarder, err := portforward.New(dialer, ports, stopChan, readyChan, out, errOut)
	if err != nil {
		return PodPortForwarder{}, err
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
			panic(errOut.String())
		} else if len(out.String()) != 0 {
			localPort, _ = parsePort(out.String())
		}
		log.Infof("Port forwarding started between %s:%s", localPort, remotePort)
	case err := <-errorChan:
		log.Errorf("Error during port-forwarding: %v", err)
		close(stopChan)
		return PodPortForwarder{}, err
	}

	return PodPortForwarder{
		PodName:   podName,
		LocalPort: localPort,
		StopChan:  stopChan,
	}, nil
}

func (ppf *PodPortForwarder) CancelPodPortForwarder() {
	close(ppf.StopChan)
}
