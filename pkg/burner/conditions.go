// Copyright 2025 The Kube-burner Authors.
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
	"fmt"

	"github.com/kube-burner/kube-burner/pkg/config"
)

const (
	jqQueryConditionStringFieldPattern        = "(.conditions.[] | select(.type == \"%s\")).%s"
	jqQueryConditionLastTransitionTimePattern = "(.conditions.[] | select(.type == \"%s\")).lastTransitionTime | strptime(\"%%Y-%%m-%%dT%%H:%%M:%%SZ\") | mktime %s %d | tostring"
)

type ConditionField string

const (
	conditionFieldStatus ConditionField = "status"
	conditionFieldReason ConditionField = "reason"
)

type ConditionType string

const (
	conditionTypeReady  ConditionType = "Ready"
	conditionTypePaused ConditionType = "Paused"
)

type ConditionCheckParam struct {
	conditionField ConditionField
	expectedValue  string
}

func newConditionCheckParam(field ConditionField, value string) ConditionCheckParam {
	return ConditionCheckParam{conditionField: field, expectedValue: value}
}

func (ccp *ConditionCheckParam) toStatusPath(conditionType ConditionType) config.StatusPath {
	return config.StatusPath{
		Key:   fmt.Sprintf(jqQueryConditionStringFieldPattern, conditionType, ccp.conditionField),
		Value: ccp.expectedValue,
	}
}

type ConditionCheckConfig struct {
	conditionType        ConditionType
	conditionCheckParams []ConditionCheckParam
	timeGreaterThan      bool
}

func (ccc *ConditionCheckConfig) toStatusPaths(timeUTC int64) []config.StatusPath {
	timeComparator := ">="
	if ccc.timeGreaterThan {
		timeComparator = ">"
	}
	statusPath := []config.StatusPath{
		{
			Key:   fmt.Sprintf(jqQueryConditionLastTransitionTimePattern, ccc.conditionType, timeComparator, timeUTC),
			Value: "true",
		},
	}
	for _, cpp := range ccc.conditionCheckParams {
		statusPath = append(
			statusPath,
			cpp.toStatusPath(ccc.conditionType),
		)
	}
	return statusPath
}
