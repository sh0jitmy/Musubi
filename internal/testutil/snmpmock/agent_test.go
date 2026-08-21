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

package snmpmock

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestMockAgent_LifecycleAndOIDs(t *testing.T) {
	t.Parallel()

	agent := NewMockAgent(map[string]any{
		"1.3.6.1.2.1.1.1.0": "RouterOS",
	})

	addr, err := agent.Start()
	require.NoError(t, err)
	assert.NotEmpty(t, addr)

	val, ok := agent.GetOID("1.3.6.1.2.1.1.1.0")
	assert.True(t, ok)
	assert.Equal(t, "RouterOS", val)

	agent.SetOID("1.3.6.1.2.1.1.5.0", "spine1")
	val, ok = agent.GetOID("1.3.6.1.2.1.1.5.0")
	assert.True(t, ok)
	assert.Equal(t, "spine1", val)

	agent.Stop()
}
