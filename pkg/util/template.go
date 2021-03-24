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
	"math/rand"
	"text/template"
	"time"
)

type templateOption string

const (
	MissingKeyError templateOption = "missingkey=error"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

//RenderTemplate renders a go-template and adds several custom functions
func RenderTemplate(original []byte, inputData interface{}, options templateOption) ([]byte, error) {
	var rendered bytes.Buffer
	funcMap := template.FuncMap{
		"add": func(a int, b int) int {
			return a + b
		},
		"multiply": func(a int, b int) int {
			return a * b
		},
		"rand": func(length int) string {
			var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
			b := make([]rune, length)
			for i := range b {
				b[i] = letterRunes[rand.Intn(len(letterRunes))]
			}
			return string(b)
		},
		"sequence": func(start int, end int) []int {
			var sequence = []int{}
			for i := start; i <= end; i++ {
				sequence = append(sequence, i)
			}
			return sequence
		},
	}
	t, err := template.New("").Option(string(options)).Funcs(funcMap).Parse(string(original))
	if err != nil {
		return nil, fmt.Errorf("Parsing error: %s", err)
	}
	err = t.Execute(&rendered, inputData)
	if err != nil {
		return nil, fmt.Errorf("Rendering error: %s", err)
	}
	return rendered.Bytes(), nil
}
