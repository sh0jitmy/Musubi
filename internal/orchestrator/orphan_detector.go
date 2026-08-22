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

// OrphanScenarioItem represents a scenario referencing missing targets
type OrphanScenarioItem struct {
	ScenarioID         string   `json:"scenario_id"`
	ScenarioName       string   `json:"scenario_name"`
	MissingTargetNames []string `json:"missing_target_names"`
}

// DetectOrphans identifies scenarios that reference targets not present or DELETED in active inventory
func DetectOrphans(
	scenarios []struct {
		ID      string
		Name    string
		Targets []string
	},
	activeTargets map[string]bool,
) []OrphanScenarioItem {
	var orphans []OrphanScenarioItem
	for _, sc := range scenarios {
		var missing []string
		for _, t := range sc.Targets {
			if !activeTargets[t] {
				missing = append(missing, t)
			}
		}
		if len(missing) > 0 {
			orphans = append(orphans, OrphanScenarioItem{
				ScenarioID:         sc.ID,
				ScenarioName:       sc.Name,
				MissingTargetNames: missing,
			})
		}
	}
	return orphans
}
