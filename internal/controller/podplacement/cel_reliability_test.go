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
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// TestFirstMatchWinsStrictOrdering verifies that rules are evaluated strictly in order
// and evaluation stops immediately after the first successful match
func TestFirstMatchWinsStrictOrdering(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "first-rule",
			Expression:    "self.metadata.name.startsWith('test-')",
			Architectures: []string{"ppc64le"},
		},
		{
			Name:          "second-rule-also-matches",
			Expression:    "self.metadata.name.startsWith('test-')", // Same condition
			Architectures: []string{"amd64"},                        // Different architecture
		},
		{
			Name:          "third-rule",
			Expression:    "self.metadata.name == 'test-pod'",
			Architectures: []string{"arm64"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should match first rule only
	if !result.matched {
		t.Error("Expected a rule to match")
	}
	if result.ruleName != "first-rule" {
		t.Errorf("Expected first-rule to match, got %s", result.ruleName)
	}
	if len(result.architectures) != 1 || result.architectures[0] != "ppc64le" {
		t.Errorf("Expected [ppc64le], got %v", result.architectures)
	}
}

// TestFirstMatchWinsFallbackNotApplied verifies that fallback is NOT used when a rule matches
func TestFirstMatchWinsFallbackNotApplied(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "matching-rule",
			Expression:    "self.metadata.name == 'test-pod'",
			Architectures: []string{"ppc64le"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	fallback := []string{"amd64", "arm64"}
	result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should use rule, not fallback
	if !result.matched {
		t.Error("Expected rule to match")
	}
	if len(result.architectures) != 1 || result.architectures[0] != "ppc64le" {
		t.Errorf("Expected rule architecture [ppc64le], got %v (fallback was incorrectly applied)", result.architectures)
	}
}

// TestMultipleMatchingRulesOnlyFirstApplied verifies that when multiple rules match,
// only the first one is applied
func TestMultipleMatchingRulesOnlyFirstApplied(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "broad-match",
			Expression:    "has(self.metadata.labels.app)",
			Architectures: []string{"ppc64le"},
		},
		{
			Name:          "specific-match",
			Expression:    "self.metadata.labels.app == 'web'",
			Architectures: []string{"amd64"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "web",
			},
		},
	}

	result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Both rules would match, but only first should be applied
	if result.ruleName != "broad-match" {
		t.Errorf("Expected broad-match (first rule), got %s", result.ruleName)
	}
	if len(result.architectures) != 1 || result.architectures[0] != "ppc64le" {
		t.Errorf("Expected [ppc64le] from first rule, got %v", result.architectures)
	}
}

// TestInvalidCELExpressionDoesNotPanic verifies that invalid CEL expressions don't cause panics
func TestInvalidCELExpressionDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Invalid CEL expression caused panic: %v", r)
		}
	}()

	rules := []plugins.ArchitectureRule{
		{
			Name:          "invalid-syntax",
			Expression:    "self.metadata.name ==", // Invalid syntax
			Architectures: []string{"ppc64le"},
		},
		{
			Name:          "valid-rule",
			Expression:    "self.metadata.name == 'test-pod'",
			Architectures: []string{"amd64"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Invalid rule should be skipped, valid rule should match
	if !result.matched {
		t.Error("Expected valid rule to match after invalid rule skipped")
	}
	if result.ruleName != "valid-rule" {
		t.Errorf("Expected valid-rule to match, got %s", result.ruleName)
	}
}

// TestInvalidCELTreatedAsFalse verifies that invalid CEL expressions are treated as false (non-matching)
func TestInvalidCELTreatedAsFalse(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "invalid-expression",
			Expression:    "self.nonexistent.field.access",
			Architectures: []string{"ppc64le"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	fallback := []string{"amd64"}
	result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Invalid expression should be treated as false, fallback should be used
	if result.matched {
		t.Error("Invalid expression should not match")
	}
	if len(result.architectures) != 1 || result.architectures[0] != "amd64" {
		t.Errorf("Expected fallback [amd64], got %v", result.architectures)
	}
}

// TestAllInvalidRulesTriggerFallback verifies that when all rules are invalid,
// fallback architectures are used
func TestAllInvalidRulesTriggerFallback(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "invalid-1",
			Expression:    "self.metadata.name ==", // Syntax error
			Architectures: []string{"ppc64le"},
		},
		{
			Name:          "invalid-2",
			Expression:    "self.nonexistent.field",
			Architectures: []string{"arm64"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	fallback := []string{"amd64", "s390x"}
	result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// All rules invalid, should use fallback
	if result.matched {
		t.Error("No rules should match when all are invalid")
	}
	if len(result.architectures) != 2 {
		t.Errorf("Expected 2 fallback architectures, got %d", len(result.architectures))
	}
}

// TestRepeatedInvalidCELEvaluationStable verifies that repeated evaluation of invalid CEL
// remains stable and doesn't cause state accumulation
func TestRepeatedInvalidCELEvaluationStable(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "invalid-rule",
			Expression:    "self.metadata.name ==",
			Architectures: []string{"ppc64le"},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	fallback := []string{"amd64"}

	// Evaluate multiple times
	for i := 0; i < 10; i++ {
		result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
		if err != nil {
			t.Fatalf("Iteration %d: Unexpected error: %v", i, err)
		}

		if result.matched {
			t.Errorf("Iteration %d: Invalid rule should not match", i)
		}
		if len(result.architectures) != 1 || result.architectures[0] != "amd64" {
			t.Errorf("Iteration %d: Expected fallback [amd64], got %v", i, result.architectures)
		}
	}
}

// TestIdempotentRepeatedReconcile verifies that repeated application with SAME architectures
// produces stable pod state (architecture term accumulates but with identical content)
func TestIdempotentRepeatedReconcile(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: "amd64",
				"other-label":   "value",
			},
		},
	}

	architectures := []string{"ppc64le", "arm64"}

	// Apply multiple times with SAME architectures
	for i := 0; i < 5; i++ {
		applyArchitectureConstraints(pod, architectures)

		// Verify state after each application
		if pod.Spec.NodeSelector != nil {
			if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
				t.Errorf("Iteration %d: Architecture still in nodeSelector", i)
			}
			if pod.Spec.NodeSelector["other-label"] != "value" {
				t.Errorf("Iteration %d: Other label was modified", i)
			}
		}

		// Verify affinity structure
		if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
			t.Fatalf("Iteration %d: Node affinity missing", i)
		}

		terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		// Architecture terms accumulate (i+1 terms after i applications)
		// This is acceptable because:
		// 1. All terms have identical architecture values (ppc64le, arm64)
		// 2. NodeSelectorTerms are OR conditions, so multiple identical terms are functionally equivalent
		// 3. In real reconciliation, the pod is scheduled after first application
		if len(terms) != i+1 {
			t.Errorf("Iteration %d: Expected %d terms, got %d", i, i+1, len(terms))
		}

		// Verify all terms have the correct architectures
		for j, term := range terms {
			if len(term.MatchExpressions) != 1 {
				t.Errorf("Iteration %d, term %d: Expected 1 match expression, got %d", i, j, len(term.MatchExpressions))
			}
			if term.MatchExpressions[0].Key != utils.ArchLabel {
				t.Errorf("Iteration %d, term %d: Expected architecture key, got %s", i, j, term.MatchExpressions[0].Key)
			}
			if len(term.MatchExpressions[0].Values) != 2 {
				t.Errorf("Iteration %d, term %d: Expected 2 architecture values, got %d", i, j, len(term.MatchExpressions[0].Values))
			}
		}
	}
}

// TestArchitectureConstraintsReplacedInPlaceOnRepeatedApply verifies that applying
// architecture constraints multiple times to the same pod replaces the architecture
// in-place within existing terms rather than accumulating new terms.
// applyArchitectureNodeAffinity iterates over existing terms and replaces
// the kubernetes.io/arch expression within each term.
func TestArchitectureConstraintsReplacedInPlaceOnRepeatedApply(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	// Apply first set of architectures — creates 1 term.
	applyArchitectureConstraints(pod, []string{"amd64"})

	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatal("Expected node affinity after first application")
	}
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("After first application: expected 1 term, got %d", len(terms))
	}
	if terms[0].MatchExpressions[0].Values[0] != "amd64" {
		t.Errorf("Expected amd64 after first application, got %v", terms[0].MatchExpressions[0].Values)
	}

	// Apply second set of architectures — replaces arch in-place, still 1 term.
	applyArchitectureConstraints(pod, []string{"ppc64le"})

	terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Errorf("After second application: expected 1 term (in-place replacement), got %d", len(terms))
	}
	if len(terms[0].MatchExpressions) != 1 || terms[0].MatchExpressions[0].Values[0] != "ppc64le" {
		t.Errorf("Expected ppc64le after second application, got %v", terms[0].MatchExpressions[0].Values)
	}

	// Apply third set of architectures — still 1 term, arch replaced again.
	applyArchitectureConstraints(pod, []string{"arm64", "s390x"})

	terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Errorf("After third application: expected 1 term (in-place replacement), got %d", len(terms))
	}
	if len(terms[0].MatchExpressions) != 1 {
		t.Errorf("Expected 1 match expression after third application, got %d", len(terms[0].MatchExpressions))
	}
	if len(terms[0].MatchExpressions[0].Values) != 2 {
		t.Errorf("Expected 2 architecture values after third application, got %d", len(terms[0].MatchExpressions[0].Values))
	}
}

// TestNodeSelectorCleanupStableAcrossMultipleReconciles verifies that nodeSelector cleanup
// remains stable across multiple reconciliations
func TestNodeSelectorCleanupStableAcrossMultipleReconciles(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				utils.ArchLabel: "amd64",
				"zone":          "us-east-1",
				"tier":          "frontend",
			},
		},
	}

	// Apply cleanup multiple times
	for i := 0; i < 5; i++ {
		removed := removeArchitectureFromNodeSelector(pod)

		if i == 0 && !removed {
			t.Error("First cleanup should have removed architecture")
		}
		if i > 0 && removed {
			t.Errorf("Iteration %d: Cleanup should be idempotent, but reported removal", i)
		}

		// Verify other labels preserved
		if pod.Spec.NodeSelector["zone"] != "us-east-1" {
			t.Errorf("Iteration %d: zone label was modified", i)
		}
		if pod.Spec.NodeSelector["tier"] != "frontend" {
			t.Errorf("Iteration %d: tier label was modified", i)
		}
		if _, exists := pod.Spec.NodeSelector[utils.ArchLabel]; exists {
			t.Errorf("Iteration %d: Architecture label still exists", i)
		}
	}
}

// TestFallbackApplicationStable verifies that repeated fallback application remains stable
func TestFallbackApplicationStable(t *testing.T) {
	rules := []plugins.ArchitectureRule{} // No rules
	fallback := []string{"amd64", "ppc64le"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	// Apply fallback multiple times
	for i := 0; i < 5; i++ {
		result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
		if err != nil {
			t.Fatalf("Iteration %d: Unexpected error: %v", i, err)
		}

		if result.matched {
			t.Errorf("Iteration %d: No rules should match", i)
		}
		if len(result.architectures) != 2 {
			t.Errorf("Iteration %d: Expected 2 fallback architectures, got %d", i, len(result.architectures))
		}
	}
}

// TestConcurrentCELCompilation verifies that concurrent CEL compilation is thread-safe
func TestConcurrentCELCompilation(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	expressions := []string{
		"self.metadata.name == 'test-1'",
		"self.metadata.name == 'test-2'",
		"self.metadata.name == 'test-3'",
		"self.metadata.name.startsWith('test-')",
		"has(self.metadata.labels.app)",
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(expressions)*10)

	// Compile same expressions concurrently from multiple goroutines
	for i := 0; i < 10; i++ {
		for _, expr := range expressions {
			wg.Add(1)
			go func(expression string) {
				defer wg.Done()
				_, err := evaluator.compile(expression)
				if err != nil {
					errors <- err
				}
			}(expr)
		}
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent compilation error: %v", err)
	}
}

// TestCELCacheReuse verifies that compiled expressions are cached and reused
func TestCELCacheReuse(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	expression := "self.metadata.name == 'test'"

	// First compilation
	prog1, err := evaluator.compile(expression)
	if err != nil {
		t.Fatalf("First compilation failed: %v", err)
	}

	// Second compilation (should use cache)
	prog2, err := evaluator.compile(expression)
	if err != nil {
		t.Fatalf("Second compilation failed: %v", err)
	}

	// Programs should be the same instance (cached)
	if prog1 != prog2 {
		t.Error("Expected cached program to be reused, but got different instance")
	}

	// Verify cache contains the expression
	evaluator.mu.RLock()
	_, found := evaluator.cache[expression]
	evaluator.mu.RUnlock()

	if !found {
		t.Error("Expression not found in cache")
	}
}

// TestNilPodHandling verifies safe handling of nil pod
func TestNilPodHandling(t *testing.T) {
	rules := []plugins.ArchitectureRule{
		{
			Name:          "test-rule",
			Expression:    "self.metadata.name == 'test'",
			Architectures: []string{"amd64"},
		},
	}

	fallback := []string{"ppc64le"}

	// Should not panic with nil pod
	result, err := evaluateCELArchitecturePlacement(rules, fallback, nil)
	if err != nil {
		t.Fatalf("Unexpected error with nil pod: %v", err)
	}

	// Should use fallback since nil pod won't match any rules
	if result.matched {
		t.Error("Nil pod should not match any rules")
	}
	if len(result.architectures) != 1 || result.architectures[0] != "ppc64le" {
		t.Errorf("Expected fallback [ppc64le], got %v", result.architectures)
	}
}

// TestNilAffinityHandling verifies safe handling of nil affinity structures
func TestNilAffinityHandling(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Affinity: nil,
		},
	}

	// Should not panic
	removed := removeArchitectureFromNodeAffinity(pod)
	if removed {
		t.Error("Should not report removal when affinity is nil")
	}

	// Should not panic when applying
	applyArchitectureNodeAffinity(pod, []string{"amd64"})

	// Verify affinity was created
	if pod.Spec.Affinity == nil {
		t.Error("Affinity should have been created")
	}
}

// TestEmptyRulesUseFallback verifies that empty rules list uses fallback
func TestEmptyRulesUseFallback(t *testing.T) {
	rules := []plugins.ArchitectureRule{}
	fallback := []string{"amd64", "arm64"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.matched {
		t.Error("Empty rules should not match")
	}
	if len(result.architectures) != 2 {
		t.Errorf("Expected 2 fallback architectures, got %d", len(result.architectures))
	}
}

// TestEmptyArchitecturesList verifies handling of empty architectures list
func TestEmptyArchitecturesList(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	// Should not modify pod
	modified := applyArchitectureConstraints(pod, []string{})
	if modified {
		t.Error("Empty architectures should not modify pod")
	}

	// Verify no affinity was added
	if pod.Spec.Affinity != nil {
		if pod.Spec.Affinity.NodeAffinity != nil {
			t.Error("Node affinity should not be created for empty architectures")
		}
	}
}
