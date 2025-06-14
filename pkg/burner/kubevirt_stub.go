//go:build !no_kubevirt
// +build !no_kubevirt

package burner

import (
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
)

func (ex *Executor) setupKubeVirtJob(mapper interface{}) {
	// Stub implementation that does nothing
}

func waitForKubeVirtVMIs(ex *Executor, readySelector map[string]string, timeout time.Duration, errorsChannel chan config.ErrorMap) {
	// Stub implementation that does nothing
}

func (ex *Executor) setupKubeVirtClient() error {
	return nil
}
