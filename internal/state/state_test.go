// Copyright 2026 Musubi Contributors
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
// Author: sh0jitmy

package state

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestStateRepository_SetAndGet(t *testing.T) {
	t.Parallel()

	var transitions []types.StateTransition
	repo := NewRepository(func(st types.StateTransition) {
		transitions = append(transitions, st)
	})

	// Set raw
	changed := repo.SetRaw("spine1", "ifOperStatus.1", "up", "TRAP")
	assert.True(t, changed)
	assert.Len(t, transitions, 1)
	assert.Equal(t, "spine1", transitions[0].Target)
	assert.Equal(t, "up", transitions[0].NewValue)

	// Set unchanged
	changed = repo.SetRaw("spine1", "ifOperStatus.1", "up", "TRAP")
	assert.False(t, changed)
	assert.Len(t, transitions, 1)

	// Get raw
	val, ok := repo.GetRaw("spine1", "ifOperStatus.1")
	assert.True(t, ok)
	assert.Equal(t, "up", val)

	_, ok = repo.GetRaw("unknown", "key")
	assert.False(t, ok)

	// Set & Get derived
	repo.SetDerived("cluster.health", "HEALTHY")
	derivedMap := repo.GetDerivedMap()
	assert.Equal(t, "HEALTHY", derivedMap["cluster.health"])

	rawMap := repo.GetRawMap()
	assert.Equal(t, "up", rawMap["spine1"]["ifOperStatus.1"])
}

func TestCELEvaluator_Evaluate(t *testing.T) {
	t.Parallel()

	eval, err := NewEvaluator()
	require.NoError(t, err)

	raw := map[string]map[string]any{
		"spine1": {
			"status": 1,
		},
	}
	derived := map[string]any{
		"is_primary": true,
	}
	inputs := map[string]any{
		"expected": 1,
	}

	res, err := eval.Evaluate("raw['spine1']['status'] == inputs['expected']", raw, derived, inputs)
	require.NoError(t, err)
	assert.True(t, res)

	res, err = eval.Evaluate("derived['is_primary'] == true", raw, derived, inputs)
	require.NoError(t, err)
	assert.True(t, res)

	// Invalid expression syntax
	_, err = eval.Evaluate("invalid && syntax !!!", raw, derived, inputs)
	require.Error(t, err)

	// Non-boolean return expression
	_, err = eval.Evaluate("inputs['expected'] + 10", raw, derived, inputs)
	require.Error(t, err)

	// Runtime evaluation error (e.g. division by zero)
	_, err = eval.Evaluate("1 / 0 == 0", raw, derived, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cel eval error")

	// Nil maps fallback test
	resNil, err := eval.Evaluate("true", nil, nil, nil)
	require.NoError(t, err)
	assert.True(t, resNil)
}

//nolint:paralleltest // overrides package-level mock
func TestEvaluator_NewEnvError(t *testing.T) {
	restore := SetCelNewEnvForTesting(func(opts ...cel.EnvOption) (*cel.Env, error) {
		return nil, fmt.Errorf("mocked new env error")
	})
	defer restore()

	eval, err := NewEvaluator()
	require.Error(t, err)
	assert.Nil(t, eval)
	assert.Contains(t, err.Error(), "mocked new env error")
}

//nolint:paralleltest // overrides package-level mock
func TestEvaluator_ProgramError(t *testing.T) {
	eval, err := NewEvaluator()
	require.NoError(t, err)

	oldProgram := celProgram
	defer func() { celProgram = oldProgram }()

	celProgram = func(env *cel.Env, ast *cel.Ast) (cel.Program, error) {
		return nil, fmt.Errorf("mocked program error")
	}

	res, err := eval.Evaluate("true == true", nil, nil, nil)
	require.Error(t, err)
	assert.False(t, res)
	assert.Contains(t, err.Error(), "mocked program error")
}
