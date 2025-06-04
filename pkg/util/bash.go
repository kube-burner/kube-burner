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

package util

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"

	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
)

func RunShellCmd(shellCmdLine string, embedCfg *fileutils.EmbedConfiguration, background bool) (*bytes.Buffer, *bytes.Buffer, error) {
	// Split the shell script from its args
	parts := strings.Split(shellCmdLine, " ")

	// Get a reader (embedded, local or remote) for the contents of the file
	shellScriptReader, err := fileutils.GetScriptsReader(parts[0], embedCfg)
	if err != nil {
		return nil, nil, err
	}

	// Add the script arguments into the command
	var c []string
	if len(parts) > 1 {
		c = append([]string{"-s", "-"}, parts[1:]...)
	}

	// Create a command
	cmd := exec.Command("/bin/sh", c...)

	// Run the shell script from STDIN
	cmd.Stdin = shellScriptReader

	// Store STDOUR and STDERR to local variables
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	if background {
		// Fully detach from parent so the child keeps running if Go process exits
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
		err = cmd.Start()
		return &outb, &errb, err
	}

	// Run the command and return its return and outputs
	err = cmd.Run()
	return &outb, &errb, err
}
