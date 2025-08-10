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

package errors_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/kube-burner/kube-burner/pkg/errors"
)

func TestErrors(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Errors Suite")
}

var _ = Describe("Error Enhancement", func() {
	Describe("EnhanceYAMLParseError", func() {
		Context("when error is nil", func() {
			It("should return nil", func() {
				result := errors.EnhanceYAMLParseError("test.yml", nil)
				Expect(result).ToNot(HaveOccurred())
			})
		})

		Context("when error is yaml.TypeError", func() {
			It("should enhance with line and column information", func() {
				yamlErr := &yaml.TypeError{Errors: []string{"line 5: invalid syntax", "line 10: unknown field"}}
				result := errors.EnhanceYAMLParseError("config.yml", yamlErr)

				Expect(result).To(HaveOccurred())
				Expect(result.Error()).Should(ContainSubstring("failed to parse config file config.yml"))
				Expect(result.Error()).Should(ContainSubstring("line 5: invalid syntax; line 10: unknown field"))
			})
		})

		Context("when error is generic YAML error with line info", func() {
			It("should enhance with filename context", func() {
				yamlErr := yaml.Unmarshal([]byte("invalid: [unclosed"), &map[string]interface{}{})
				result := errors.EnhanceYAMLParseError("test.yml", yamlErr)

				Expect(result).To(HaveOccurred())
				Expect(result.Error()).Should(ContainSubstring("failed to parse config file test.yml"))
			})
		})
	})

	Describe("EnhanceJSONParseError", func() {
		Context("when error is nil", func() {
			It("should return nil", func() {
				result := errors.EnhanceJSONParseError("test.json", nil)
				Expect(result).ToNot(HaveOccurred())
			})
		})

		Context("when error is json.SyntaxError", func() {
			It("should enhance with offset information", func() {
				jsonErr := &json.SyntaxError{Offset: 42}
				result := errors.EnhanceJSONParseError("config.json", jsonErr)

				Expect(result).To(HaveOccurred())
				Expect(result.Error()).Should(ContainSubstring("failed to parse config file config.json"))
				Expect(result.Error()).Should(ContainSubstring("JSON syntax error at offset 42"))
			})
		})

		Context("when error is json.UnmarshalTypeError", func() {
			It("should enhance with field and type information", func() {
				jsonErr := &json.UnmarshalTypeError{Field: "name", Offset: 25, Type: nil, Value: "number"}
				result := errors.EnhanceJSONParseError("data.json", jsonErr)

				Expect(result).To(HaveOccurred())
				Expect(result.Error()).Should(ContainSubstring("failed to parse config file data.json"))
				Expect(result.Error()).Should(ContainSubstring("JSON type error at field 'name' (offset 25)"))
			})
		})
	})
})
