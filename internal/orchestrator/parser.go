// Copyright 2026 [Copyright Holder]
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: [YOUR_NAME]

package orchestrator

import (
	"fmt"
	"strings"

	"github.com/sh0jitmy/musubi/internal/common/types"
	"gopkg.in/yaml.v3"
)

// ParseYAML parses and validates Scenario YAML DSL
func ParseYAML(yamlData []byte) (*types.ScenarioDSL, []string, error) {
	var dsl types.ScenarioDSL
	if err := yaml.Unmarshal(yamlData, &dsl); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	if dsl.Name == "" {
		return nil, nil, fmt.Errorf("scenario name cannot be empty")
	}

	targetMap := make(map[string]bool)
	for _, t := range dsl.TargetLocks {
		if t != "" {
			targetMap[t] = true
		}
	}

	for _, step := range dsl.Steps {
		if step.Target != "" && !strings.HasPrefix(step.Target, "$") {
			targetMap[step.Target] = true
		}
	}

	var targets []string
	for t := range targetMap {
		targets = append(targets, t)
	}

	return &dsl, targets, nil
}
