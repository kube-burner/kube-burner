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

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/spf13/cast"
)

type templateOption string

const (
	MissingKeyError templateOption = "missingkey=error"
	MissingKeyZero  templateOption = "missingkey=zero"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

//RenderTemplate renders a go-template and adds several custom functions
func RenderTemplate(original []byte, inputData interface{}, options templateOption) ([]byte, error) {
	var rendered bytes.Buffer
	funcMap := sprig.GenericFuncMap()
	extraFuncs := map[string]interface{}{
		"multiply": func(a interface{}, b ...interface{}) int {
			res := cast.ToInt(a)
			for _, v := range b {
				res = res * cast.ToInt(v)
			}
			return res
		},
		"rand": func(length interface{}) string {
			var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
			b := make([]rune, cast.ToInt(length))
			for i := range b {
				b[i] = letterRunes[rand.Intn(len(letterRunes))]
			}
			return string(b)
		},
		"sequence": func(start, end interface{}) []int {
			var sequence = []int{}
			for i := cast.ToInt(start); i <= cast.ToInt(end); i++ {
				sequence = append(sequence, i)
			}
			return sequence
		},
		"randInteger": func(a, b interface{}) int {
			return rand.Intn(cast.ToInt(b)) + cast.ToInt(a)
		},
	}
	for k, v := range extraFuncs {
		funcMap[k] = v
	}
	sprig.GenericFuncMap()
	t, err := template.New("").Option(string(options)).Funcs(funcMap).Parse(string(original))
	if err != nil {
		return nil, fmt.Errorf("parsing error: %s", err)
	}
	err = t.Execute(&rendered, inputData)
	if err != nil {
		return nil, fmt.Errorf("rendering error: %s", err)
	}
	log.Tracef("Rendered template: %s", rendered.String())
	return rendered.Bytes(), nil
}
