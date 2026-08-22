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

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTelemetryMetrics(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, SNMPTrapCount)
	assert.NotNil(t, SNMPRequestCount)
	assert.NotNil(t, SNMPNetworkBytes)
	assert.NotNil(t, SNMPOperationDuration)
	assert.NotNil(t, ScenarioJobCount)
	assert.NotNil(t, StateTransitionsCount)

	// Exercise metrics
	SNMPTrapCount.WithLabelValues("spine1", "trap").Inc()
	SNMPRequestCount.WithLabelValues("spine1", "get").Inc()
	SNMPNetworkBytes.WithLabelValues("spine1", "rx").Add(128)
	SNMPOperationDuration.WithLabelValues("spine1", "get").Observe(0.015)
	ScenarioJobCount.WithLabelValues("scenario1", "SUCCESS").Inc()
	StateTransitionsCount.WithLabelValues("spine1", "trap").Inc()
}
