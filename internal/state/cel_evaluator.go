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

	"github.com/google/cel-go/cel"
)

// Evaluator evaluates Google CEL expressions against current state
type Evaluator struct {
	env *cel.Env
}

// Hooks for unit testing
var (
	celNewEnv  = cel.NewEnv
	celProgram = func(env *cel.Env, ast *cel.Ast) (cel.Program, error) {
		return env.Program(ast)
	}
)

// SetCelNewEnvForTesting allows tests to mock cel.NewEnv. Returns a restore function.
func SetCelNewEnvForTesting(fn func(opts ...cel.EnvOption) (*cel.Env, error)) func() {
	old := celNewEnv
	celNewEnv = fn
	return func() {
		celNewEnv = old
	}
}

// NewEvaluator creates a new CEL evaluator
func NewEvaluator() (*Evaluator, error) {
	env, err := celNewEnv(
		cel.Variable("raw", cel.MapType(cel.StringType, cel.MapType(cel.StringType, cel.AnyType))),
		cel.Variable("derived", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("inputs", cel.MapType(cel.StringType, cel.AnyType)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cel env: %w", err)
	}
	return &Evaluator{env: env}, nil
}

// Evaluate evaluates a CEL boolean expression against raw, derived, and input data
func (e *Evaluator) Evaluate(expr string, raw map[string]map[string]any, derived map[string]any, inputs map[string]any) (bool, error) {
	ast, iss := e.env.Compile(expr)
	if iss.Err() != nil {
		return false, fmt.Errorf("cel compile error: %w", iss.Err())
	}

	prg, err := celProgram(e.env, ast)
	if err != nil {
		return false, fmt.Errorf("cel program error: %w", err)
	}

	if raw == nil {
		raw = make(map[string]map[string]any)
	}
	if derived == nil {
		derived = make(map[string]any)
	}
	if inputs == nil {
		inputs = make(map[string]any)
	}

	out, _, err := prg.Eval(map[string]any{
		"raw":     raw,
		"derived": derived,
		"inputs":  inputs,
	})
	if err != nil {
		return false, fmt.Errorf("cel eval error: %w", err)
	}

	boolVal, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel expression '%s' did not return boolean, got %T", expr, out.Value())
	}

	return boolVal, nil
}
