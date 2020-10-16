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

package burner

import (
	"bytes"
	"log"
	"text/template"
)

func renderTemplate(original []byte, data interface{}) []byte {
	var rendered bytes.Buffer
	t, err := template.New("").Parse(string(original))
	if err != nil {
		log.Fatalf("Error parsing template: %s", err)
	}
	err = t.Execute(&rendered, data)
	if err != nil {
		log.Fatalf("Error rendering template: %s", err)
	}
	return rendered.Bytes()
}
