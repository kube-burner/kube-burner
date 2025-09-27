// Copyright 2021 The Kube-burner Authors.
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
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"text/template"

	sprig "github.com/Masterminds/sprig/v3"
	log "github.com/sirupsen/logrus"
	"gonum.org/v1/gonum/stat/combin"
)

type templateOption string

const (
	MissingKeyError templateOption = "missingkey=error"
	MissingKeyZero  templateOption = "missingkey=zero"
)

var funcMap = sprig.GenericFuncMap()

func init() {
	AddRenderingFunction("Binomial", combin.Binomial)
	AddRenderingFunction("IndexToCombination", combin.IndexToCombination)
	funcMap["GetSubnet24"] = func(subnetIdx int) string {
	    return netip.AddrFrom4([4]byte{byte(subnetIdx>>16 + 1), byte(subnetIdx >> 8), byte(subnetIdx), 0}).String() + "/24"
	}
	// Parent /16 subnet
	funcMap["GetSubnet16"] = func(subnetIdx int) string {
	    first := byte((subnetIdx >> 8) + 1)
	    second := byte(subnetIdx & 0xFF)
	    return netip.AddrFrom4([4]byte{first, second, 0, 0}).String() + "/16"
	}
	// Child /24s that stay inside the parent /16
	funcMap["GetSubnet24In16"] = func(subnetIdx, offset int) string {
	    first := byte((subnetIdx >> 8) + 1)
	    second := byte(subnetIdx & 0xFF)
	    third := byte(offset) // carve /24s by varying the 3rd octet
	    return netip.AddrFrom4([4]byte{first, second, third, 0}).String() + "/24"
	}
	// This function returns number of addresses requested per iteration from the list of total provided addresses
	funcMap["GetIPAddress"] = func(Addresses string, iteration int, addressesPerIteration int) string { // TODO Move this function to kube-burner-ocp
		var retAddrs []string
		addrSlice := strings.Split(Addresses, " ")
		for i := range addressesPerIteration {
			// For example, if iteration=6 and addressesPerIteration=2, return 12th address from list.
			// All addresses till 12th address were used in previous job iterations
			retAddrs = append(retAddrs, addrSlice[(iteration*addressesPerIteration)+i])
		}
		return strings.Join(retAddrs, " ")
	}
	funcMap["ReadFile"] = func(filePath string) (string, error) {
		// Open the file
		file, err := os.Open(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		// Read all content from the file
		content, err := io.ReadAll(file)
		if err != nil {
			return "", fmt.Errorf("failed to read file content: %w", err)
		}

		return string(content), nil
	}
}

func CleanupTemplate(original []byte) ([]byte, error) {
	// Removing all placeholders from template.
	// This needs to be done due to placeholders not being valid yaml
	if strings.TrimSpace(string(original)) == "" {
		return nil, fmt.Errorf("template is empty")
	}
	// Remove {{.*}}
	r, _ := regexp.Compile(`\{\{.*\}\}`)
	original = r.ReplaceAll(original, []byte{})
	return original, nil
}

func AddRenderingFunction(name string, function any) {
	log.Debugf("Importing template function: %s", name)
	funcMap[name] = function
}

// RenderTemplate renders a go-template
func RenderTemplate(original []byte, inputData any, options templateOption, functionTemplates []string) ([]byte, error) {
	var rendered bytes.Buffer
	t, err := template.New("").Option(string(options)).Funcs(funcMap).Parse(string(original))
	if err != nil {
		return nil, fmt.Errorf("parsing error: %s", err)
	}
	if len(functionTemplates) > 0 {
		t, err = t.ParseFiles(functionTemplates...)
		if err != nil {
			return nil, fmt.Errorf("subtemplate parsing error: %s", err)
		}
	}
	err = t.Execute(&rendered, inputData)
	if err != nil {
		return nil, fmt.Errorf("rendering error: %s", err)
	}
	log.Tracef("Rendered template: %s", rendered.String())
	return rendered.Bytes(), nil
}

// EnvToMap returns the host environment variables as a map
func EnvToMap() map[string]any {
	envMap := make(map[string]any)
	for _, v := range os.Environ() {
		envVar := strings.SplitN(v, "=", 2)
		envMap[envVar[0]] = envVar[1]
	}
	return envMap
}

// CreateFile creates a new file and writes content into it
func CreateFile(fileName string, fileContent []byte) error {
	fd, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer fd.Close()
	fd.Write(fileContent)
	return nil
}
