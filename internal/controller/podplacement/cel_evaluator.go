/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podplacement

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

// celEvaluator handles CEL expression compilation, caching, and evaluation
type celEvaluator struct {
	env   *cel.Env
	cache map[string]cel.Program
	mu    sync.RWMutex
}

var (
	// packageEvaluator is a package-level evaluator for expression caching
	packageEvaluator *celEvaluator
	// evaluatorOnce ensures the evaluator is initialized only once
	evaluatorOnce sync.Once
)

// getOrCreateEvaluator returns the package-level evaluator, creating it if necessary
// This enables expression caching across pod evaluations as specified in the enhancement
func getOrCreateEvaluator() (*celEvaluator, error) {
	var initErr error
	evaluatorOnce.Do(func() {
		packageEvaluator, initErr = newCELEvaluator()
	})
	if initErr != nil {
		return nil, initErr
	}
	return packageEvaluator, nil
}

// newCELEvaluator creates a new CEL evaluator with a Pod-aware environment
func newCELEvaluator() (*celEvaluator, error) {
	env, err := cel.NewEnv(
		cel.Variable("self", cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &celEvaluator{
		env:   env,
		cache: make(map[string]cel.Program),
	}, nil
}

// compile compiles a CEL expression and caches the result
func (e *celEvaluator) compile(expression string) (cel.Program, error) {
	// Check cache first (read lock)
	e.mu.RLock()
	if prog, found := e.cache[expression]; found {
		e.mu.RUnlock()
		return prog, nil
	}
	e.mu.RUnlock()

	// Compile expression
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}

	// Check that the expression returns a boolean
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return a boolean, got %v", ast.OutputType())
	}

	// Create program
	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}

	// Cache the compiled program (write lock)
	e.mu.Lock()
	e.cache[expression] = prog
	e.mu.Unlock()

	return prog, nil
}

// podToMap converts a Pod to a map structure that CEL can evaluate
func podToMap(pod *corev1.Pod) map[string]interface{} {
	if pod == nil {
		return map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":        "",
				"namespace":   "",
				"labels":      map[string]interface{}{},
				"annotations": map[string]interface{}{},
			},
		}
	}

	labels := make(map[string]interface{})
	for k, v := range pod.Labels {
		labels[k] = v
	}

	annotations := make(map[string]interface{})
	for k, v := range pod.Annotations {
		annotations[k] = v
	}

	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        pod.Name,
			"namespace":   pod.Namespace,
			"labels":      labels,
			"annotations": annotations,
		},
	}
}

// evaluate evaluates a CEL expression against a Pod
// Returns true if the expression matches, false otherwise
// Evaluation errors are treated as false (non-matching)
func (e *celEvaluator) evaluate(expression string, pod *corev1.Pod) (bool, error) {
	prog, err := e.compile(expression)
	if err != nil {
		return false, err
	}

	// Convert pod to a map structure for CEL evaluation
	podMap := podToMap(pod)

	// Evaluate the expression
	val, _, err := prog.Eval(map[string]interface{}{
		"self": podMap,
	})
	if err != nil {
		// Evaluation errors are treated as non-matches per enhancement doc
		return false, fmt.Errorf("CEL evaluation error: %w", err)
	}

	// Convert result to boolean
	result := ref.Val(val)
	if result.Type() != types.BoolType {
		return false, fmt.Errorf("CEL expression did not return a boolean: got %v", result.Type())
	}

	return result.Value().(bool), nil
}

// evaluateRules evaluates CEL rules in order and returns the first matching rule's architectures
// Returns nil if no rules match
func (e *celEvaluator) evaluateRules(rules []plugins.ArchitectureRule, pod *corev1.Pod) ([]string, string, error) {
	for _, rule := range rules {
		matched, err := e.evaluate(rule.Expression, pod)
		if err != nil {
			// Log the error but continue to next rule per enhancement doc
			// The caller should log this appropriately
			continue
		}
		if matched {
			// First match wins
			return rule.Architectures, rule.Name, nil
		}
	}
	// No rules matched
	return nil, "", nil
}

// evaluateResult represents the result of CEL rule evaluation
type evaluateResult struct {
	architectures []string
	ruleName      string
	matched       bool
}

// evaluateCELArchitecturePlacement evaluates the celArchitecturePlacement plugin rules
// Returns the architectures to apply and whether a rule matched
// Uses a package-level evaluator for expression caching across pod evaluations
func evaluateCELArchitecturePlacement(rules []plugins.ArchitectureRule, fallbackArchitectures []string, pod *corev1.Pod) (*evaluateResult, error) {
	if rules == nil && fallbackArchitectures == nil {
		return nil, fmt.Errorf("both rules and fallbackArchitectures are nil")
	}

	// Get or create the package-level evaluator for expression caching
	evaluator, err := getOrCreateEvaluator()
	if err != nil {
		return nil, fmt.Errorf("failed to get CEL evaluator: %w", err)
	}

	// Evaluate rules in order - detailed logging happens in the caller (cel_integration.go)
	architectures, ruleName, err := evaluator.evaluateRules(rules, pod)
	if err != nil {
		return nil, fmt.Errorf("error evaluating CEL rules: %w", err)
	}

	if architectures != nil {
		// A rule matched
		return &evaluateResult{
			architectures: architectures,
			ruleName:      ruleName,
			matched:       true,
		}, nil
	}

	// No rules matched, use fallback architectures
	return &evaluateResult{
		architectures: fallbackArchitectures,
		ruleName:      "",
		matched:       false,
	}, nil
}

// validateCELExpression validates a CEL expression without evaluating it
// This can be used for validation at admission time
func validateCELExpression(expression string) error {
	evaluator, err := newCELEvaluator()
	if err != nil {
		return err
	}

	_, err = evaluator.compile(expression)
	return err
}
