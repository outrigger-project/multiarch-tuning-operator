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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
)

func TestNewCELEvaluator(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}
	if evaluator == nil {
		t.Fatal("CEL evaluator is nil")
	}
	if evaluator.env == nil {
		t.Fatal("CEL environment is nil")
	}
	if evaluator.cache == nil {
		t.Fatal("CEL cache is nil")
	}
}

func TestCELEvaluatorCompile(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	tests := []struct {
		name        string
		expression  string
		expectError bool
	}{
		{
			name:        "valid boolean expression",
			expression:  "self.metadata.name == 'test-pod'",
			expectError: false,
		},
		{
			name:        "valid label check with has() and map access",
			expression:  "has(self.metadata.labels.app) && self.metadata.labels.app == 'web'",
			expectError: false,
		},
		{
			name:        "invalid .exists() syntax on map - should fail",
			expression:  "self.metadata.labels.exists(l, l.key == 'app' && l.value == 'web')",
			expectError: true,
		},
		{
			name:        "invalid syntax",
			expression:  "self.metadata.name ==",
			expectError: true,
		},
		{
			name:        "non-boolean return type",
			expression:  "self.metadata.name",
			expectError: true,
		},
		{
			name:        "valid label check with bracket notation",
			expression:  "has(self.metadata.labels['app.kubernetes.io/component']) && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
			expectError: false,
		},
		{
			name:        "missing label check returns false safely",
			expression:  "has(self.metadata.labels.nonexistent) && self.metadata.labels.nonexistent == 'value'",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluator.compile(tt.expression)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestCELEvaluatorCompileCaching(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	expression := "self.metadata.name == 'test'"

	// First compilation
	prog1, err := evaluator.compile(expression)
	if err != nil {
		t.Fatalf("Failed to compile expression: %v", err)
	}

	// Second compilation should use cache
	prog2, err := evaluator.compile(expression)
	if err != nil {
		t.Fatalf("Failed to compile expression: %v", err)
	}

	// Should be the same program instance from cache
	if prog1 != prog2 {
		t.Error("Expected cached program to be returned")
	}
}

func TestCELEvaluatorEvaluate(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	tests := []struct {
		name           string
		expression     string
		pod            *corev1.Pod
		expectedResult bool
		expectError    bool
	}{
		{
			name:       "match by name",
			expression: "self.metadata.name == 'nginx-pod'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "nginx-pod",
				},
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name:       "no match by name",
			expression: "self.metadata.name == 'nginx-pod'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "redis-pod",
				},
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name:       "match by label",
			expression: "has(self.metadata.labels.app) && self.metadata.labels.app == 'web'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name:       "name starts with",
			expression: "self.metadata.name.startsWith('redis-')",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "redis-master",
				},
			},
			expectedResult: true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.evaluate(tt.expression, tt.pod)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expectedResult {
				t.Errorf("Expected result %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestCELEvaluatorEvaluateRules(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	rules := []plugins.ArchitectureRule{
		{
			Name:          "postgres-rule",
			Expression:    "self.metadata.name.startsWith('postgres-')",
			Architectures: []string{"ppc64le"},
		},
		{
			Name:          "redis-rule",
			Expression:    "self.metadata.name.startsWith('redis-')",
			Architectures: []string{"amd64", "ppc64le"},
		},
	}

	tests := []struct {
		name             string
		pod              *corev1.Pod
		expectedArchs    []string
		expectedRuleName string
		expectMatch      bool
	}{
		{
			name: "match first rule",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "postgres-db",
				},
			},
			expectedArchs:    []string{"ppc64le"},
			expectedRuleName: "postgres-rule",
			expectMatch:      true,
		},
		{
			name: "match second rule",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "redis-cache",
				},
			},
			expectedArchs:    []string{"amd64", "ppc64le"},
			expectedRuleName: "redis-rule",
			expectMatch:      true,
		},
		{
			name: "no match",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "nginx-web",
				},
			},
			expectedArchs:    nil,
			expectedRuleName: "",
			expectMatch:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archs, ruleName, err := evaluator.evaluateRules(rules, tt.pod)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if tt.expectMatch {
				if archs == nil {
					t.Error("Expected architectures but got nil")
				}
				if len(archs) != len(tt.expectedArchs) {
					t.Errorf("Expected %d architectures, got %d", len(tt.expectedArchs), len(archs))
				}
				if ruleName != tt.expectedRuleName {
					t.Errorf("Expected rule name %s, got %s", tt.expectedRuleName, ruleName)
				}
			} else {
				if archs != nil {
					t.Errorf("Expected no match but got architectures: %v", archs)
				}
			}
		})
	}
}

func TestEvaluateCELArchitecturePlacement(t *testing.T) {
	tests := []struct {
		name                  string
		rules                 []plugins.ArchitectureRule
		fallbackArchitectures []string
		pod                   *corev1.Pod
		expectedArchs         []string
		expectedMatched       bool
		expectError           bool
	}{
		{
			name: "rule matches",
			rules: []plugins.ArchitectureRule{
				{
					Name:          "test-rule",
					Expression:    "self.metadata.name == 'test-pod'",
					Architectures: []string{"ppc64le"},
				},
			},
			fallbackArchitectures: []string{"amd64"},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expectedArchs:   []string{"ppc64le"},
			expectedMatched: true,
			expectError:     false,
		},
		{
			name: "no rule matches, use fallback",
			rules: []plugins.ArchitectureRule{
				{
					Name:          "test-rule",
					Expression:    "self.metadata.name == 'other-pod'",
					Architectures: []string{"ppc64le"},
				},
			},
			fallbackArchitectures: []string{"amd64"},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expectedArchs:   []string{"amd64"},
			expectedMatched: false,
			expectError:     false,
		},
		{
			name:                  "no rules, use fallback",
			rules:                 []plugins.ArchitectureRule{},
			fallbackArchitectures: []string{"amd64", "ppc64le"},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expectedArchs:   []string{"amd64", "ppc64le"},
			expectedMatched: false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluateCELArchitecturePlacement(tt.rules, tt.fallbackArchitectures, tt.pod)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != nil {
				if len(result.architectures) != len(tt.expectedArchs) {
					t.Errorf("Expected %d architectures, got %d", len(tt.expectedArchs), len(result.architectures))
				}
				if result.matched != tt.expectedMatched {
					t.Errorf("Expected matched=%v, got %v", tt.expectedMatched, result.matched)
				}
			}
		})
	}
}

func TestValidateCELExpression(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		expectError bool
	}{
		{
			name:        "valid expression",
			expression:  "self.metadata.name == 'test'",
			expectError: false,
		},
		{
			name:        "invalid syntax",
			expression:  "self.metadata.name ==",
			expectError: true,
		},
		{
			name:        "non-boolean return",
			expression:  "self.metadata.name",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCELExpression(tt.expression)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestCELEvaluatorNegativeCases tests error conditions and edge cases
func TestCELEvaluatorNegativeCases(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	tests := []struct {
		name        string
		expression  string
		pod         *corev1.Pod
		expectError bool
		description string
	}{
		{
			name:        "nil pod",
			expression:  "self.metadata.name == 'test'",
			pod:         nil,
			expectError: false, // We handle nil pods gracefully by returning empty metadata
			description: "Should handle nil pod gracefully",
		},
		{
			name:        "empty expression",
			expression:  "",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: true,
			description: "Should reject empty expression",
		},
		{
			name:        "malformed CEL syntax",
			expression:  "self.metadata.name ==",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: true,
			description: "Should reject malformed syntax",
		},
		{
			name:        "undefined field access",
			expression:  "self.metadata.nonexistent == 'value'",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: true,
			description: "Should handle undefined field access",
		},
		{
			name:        "type mismatch",
			expression:  "self.metadata.name + 123",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: true,
			description: "Should detect type mismatches",
		},
		{
			name:        "missing label key",
			expression:  "has(self.metadata.labels.nonexistent)",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: false,
			description: "Should handle missing label keys with has()",
		},
		{
			name:        "nil labels map",
			expression:  "has(self.metadata.labels.app)",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: nil}},
			expectError: false,
			description: "Should handle nil labels map",
		},
		{
			name:        "empty labels map",
			expression:  "has(self.metadata.labels.app)",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{}}},
			expectError: false,
			description: "Should handle empty labels map",
		},
		{
			name:        "nil annotations map",
			expression:  "has(self.metadata.annotations.key)",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Annotations: nil}},
			expectError: false,
			description: "Should handle nil annotations map",
		},
		{
			name:        "special characters in name",
			expression:  "self.metadata.name == 'test-pod_123.example'",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod_123.example"}},
			expectError: false,
			description: "Should handle special characters in names",
		},
		{
			name:        "unicode in labels",
			expression:  "has(self.metadata.labels.app) && self.metadata.labels.app == 'тест'",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"app": "тест"}}},
			expectError: false,
			description: "Should handle unicode in label values",
		},
		{
			name:        "very long expression",
			expression:  "self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test'",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError: false,
			description: "Should handle long expressions",
		},
		{
			name:        "complex boolean logic",
			expression:  "(self.metadata.name == 'test' || self.metadata.name == 'prod') && (has(self.metadata.labels.app) || has(self.metadata.labels.tier))",
			pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"app": "web"}}},
			expectError: false,
			description: "Should handle complex boolean logic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluator.evaluate(tt.expression, tt.pod)
			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}
		})
	}
}

// TestEvaluateCELArchitecturePlacementEdgeCases tests edge cases in rule evaluation
func TestEvaluateCELArchitecturePlacementEdgeCases(t *testing.T) {
	tests := []struct {
		name                  string
		rules                 []plugins.ArchitectureRule
		fallbackArchitectures []string
		pod                   *corev1.Pod
		expectError           bool
		expectedArchs         []string
		expectedMatched       bool
		description           string
	}{
		{
			name:                  "nil rules and nil fallback",
			rules:                 nil,
			fallbackArchitectures: nil,
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           true,
			description:           "Should reject nil rules and fallback",
		},
		{
			name:                  "empty rules with fallback",
			rules:                 []plugins.ArchitectureRule{},
			fallbackArchitectures: []string{"amd64"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           false,
			expectedArchs:         []string{"amd64"},
			expectedMatched:       false,
			description:           "Should use fallback with empty rules",
		},
		{
			name: "all rules fail to match",
			rules: []plugins.ArchitectureRule{
				{Name: "rule1", Expression: "self.metadata.name == 'nomatch1'", Architectures: []string{"ppc64le"}},
				{Name: "rule2", Expression: "self.metadata.name == 'nomatch2'", Architectures: []string{"s390x"}},
			},
			fallbackArchitectures: []string{"amd64"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           false,
			expectedArchs:         []string{"amd64"},
			expectedMatched:       false,
			description:           "Should use fallback when no rules match",
		},
		{
			name: "first rule has invalid expression",
			rules: []plugins.ArchitectureRule{
				{Name: "invalid", Expression: "invalid syntax", Architectures: []string{"ppc64le"}},
				{Name: "valid", Expression: "self.metadata.name == 'test'", Architectures: []string{"amd64"}},
			},
			fallbackArchitectures: []string{"s390x"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           false,
			expectedArchs:         []string{"amd64"},
			expectedMatched:       true,
			description:           "Should skip invalid rule and continue to next",
		},
		{
			name: "rule with empty architectures list",
			rules: []plugins.ArchitectureRule{
				{Name: "empty-arch", Expression: "self.metadata.name == 'test'", Architectures: []string{}},
			},
			fallbackArchitectures: []string{"amd64"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           false,
			expectedArchs:         []string{},
			expectedMatched:       true,
			description:           "Should handle empty architectures list",
		},
		{
			name: "multiple architectures in single rule",
			rules: []plugins.ArchitectureRule{
				{Name: "multi-arch", Expression: "self.metadata.name == 'test'", Architectures: []string{"amd64", "arm64", "ppc64le", "s390x"}},
			},
			fallbackArchitectures: []string{"amd64"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			expectError:           false,
			expectedArchs:         []string{"amd64", "arm64", "ppc64le", "s390x"},
			expectedMatched:       true,
			description:           "Should handle multiple architectures",
		},
		{
			name: "pod with no metadata",
			rules: []plugins.ArchitectureRule{
				{Name: "rule1", Expression: "self.metadata.name == 'test'", Architectures: []string{"amd64"}},
			},
			fallbackArchitectures: []string{"ppc64le"},
			pod:                   &corev1.Pod{},
			expectError:           false,
			expectedArchs:         []string{"ppc64le"},
			expectedMatched:       false,
			description:           "Should handle pod with no metadata",
		},
		{
			name: "pod with empty name",
			rules: []plugins.ArchitectureRule{
				{Name: "rule1", Expression: "self.metadata.name == ''", Architectures: []string{"amd64"}},
			},
			fallbackArchitectures: []string{"ppc64le"},
			pod:                   &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: ""}},
			expectError:           false,
			expectedArchs:         []string{"amd64"},
			expectedMatched:       true,
			description:           "Should handle pod with empty name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluateCELArchitecturePlacement(tt.rules, tt.fallbackArchitectures, tt.pod)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
				return
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
				return
			}
			if tt.expectError {
				return
			}

			if result == nil {
				t.Errorf("%s: result is nil", tt.description)
				return
			}

			if result.matched != tt.expectedMatched {
				t.Errorf("%s: expected matched=%v, got %v", tt.description, tt.expectedMatched, result.matched)
			}

			if len(result.architectures) != len(tt.expectedArchs) {
				t.Errorf("%s: expected %d architectures, got %d", tt.description, len(tt.expectedArchs), len(result.architectures))
				return
			}

			for i, arch := range tt.expectedArchs {
				if result.architectures[i] != arch {
					t.Errorf("%s: expected architecture[%d]=%s, got %s", tt.description, i, arch, result.architectures[i])
				}
			}
		})
	}
}

// TestCELEvaluatorConcurrency tests thread safety of CEL evaluator
func TestCELEvaluatorConcurrency(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	expression := "self.metadata.name.startsWith('test-')"

	// Run multiple goroutines concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			}
			_, err := evaluator.evaluate(expression, pod)
			if err != nil {
				t.Errorf("Goroutine %d: unexpected error: %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestCELEvaluatorRealWorldScenarios tests real-world production scenarios from enhancement doc
func TestCELEvaluatorRealWorldScenarios(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	tests := []struct {
		name           string
		expression     string
		pod            *corev1.Pod
		expectedResult bool
		description    string
	}{
		{
			name:       "operator namespace - openshift-operators",
			expression: "self.metadata.namespace == 'openshift-operators'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "operator-pod",
					Namespace: "openshift-operators",
				},
			},
			expectedResult: true,
			description:    "Should match pods in openshift-operators namespace",
		},
		{
			name:       "well-known label - app component",
			expression: "has(self.metadata.labels.app) && self.metadata.labels.app == 'database'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "db-pod",
					Labels: map[string]string{
						"app": "database",
					},
				},
			},
			expectedResult: true,
			description:    "Should match app label",
		},
		{
			name:       "well-known label - component",
			expression: "has(self.metadata.labels.component) && self.metadata.labels.component == 'postgresql'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "postgres-pod",
					Labels: map[string]string{
						"component": "postgresql",
					},
				},
			},
			expectedResult: true,
			description:    "Should match component label",
		},
		{
			name:       "combined labels - app and component",
			expression: "has(self.metadata.labels.app) && self.metadata.labels.app == 'database' && has(self.metadata.labels.component) && self.metadata.labels.component == 'postgresql'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "postgres-db",
					Labels: map[string]string{
						"app":       "database",
						"component": "postgresql",
					},
				},
			},
			expectedResult: true,
			description:    "Should match multiple labels combined",
		},
		{
			name:       "tier and environment labels",
			expression: "has(self.metadata.labels.tier) && self.metadata.labels.tier == 'frontend' && has(self.metadata.labels.environment) && self.metadata.labels.environment == 'production'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "frontend-prod",
					Labels: map[string]string{
						"tier":        "frontend",
						"environment": "production",
					},
				},
			},
			expectedResult: true,
			description:    "Should match tier and environment labels",
		},
		{
			name:       "priority label - critical",
			expression: "has(self.metadata.labels.priority) && self.metadata.labels.priority == 'critical'",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "critical-service",
					Labels: map[string]string{
						"priority": "critical",
					},
				},
			},
			expectedResult: true,
			description:    "Should match priority label",
		},
		{
			name:       "OR condition - backend with gold SLA",
			expression: "has(self.metadata.labels.priority) && self.metadata.labels.priority == 'critical' || (has(self.metadata.labels.tier) && self.metadata.labels.tier == 'backend' && has(self.metadata.labels.sla) && self.metadata.labels.sla == 'gold')",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "backend-gold",
					Labels: map[string]string{
						"tier": "backend",
						"sla":  "gold",
					},
				},
			},
			expectedResult: true,
			description:    "Should match OR condition with multiple labels",
		},
		{
			name:       "name prefix - redis pods",
			expression: "self.metadata.name.startsWith('redis-')",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "redis-master-0",
				},
			},
			expectedResult: true,
			description:    "Should match name prefix for StatefulSet pods",
		},
		{
			name:       "name contains pattern",
			expression: "self.metadata.name.contains('database')",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-database-pod-123",
				},
			},
			expectedResult: true,
			description:    "Should match name containing pattern",
		},
		{
			name:       "namespace prefix match",
			expression: "self.metadata.namespace.startsWith('prod-')",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-pod",
					Namespace: "prod-apps",
				},
			},
			expectedResult: true,
			description:    "Should match namespace prefix",
		},
		{
			name:       "label exists check only",
			expression: "has(self.metadata.labels.migrationReady)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "migrating-pod",
					Labels: map[string]string{
						"migrationReady": "true",
					},
				},
			},
			expectedResult: true,
			description:    "Should check if label exists",
		},
		{
			name:       "label does not exist",
			expression: "!has(self.metadata.labels.legacy)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "modern-pod",
					Labels: map[string]string{},
				},
			},
			expectedResult: true,
			description:    "Should match when label does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.evaluate(tt.expression, tt.pod)
			if err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
				return
			}
			if result != tt.expectedResult {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedResult, result)
			}
		})
	}
}

// TestCELMapAccessSyntax verifies that CEL expressions use correct map access syntax
// for labels and annotations, not the .exists() method which doesn't work on maps
func TestCELMapAccessSyntax(t *testing.T) {
	evaluator, err := newCELEvaluator()
	if err != nil {
		t.Fatalf("Failed to create CEL evaluator: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":                         "web",
				"tier":                        "frontend",
				"app.kubernetes.io/component": "database",
				"app.kubernetes.io/part-of":   "wordpress",
			},
			Annotations: map[string]string{
				"description": "test annotation",
			},
		},
	}

	tests := []struct {
		name           string
		expression     string
		expectedResult bool
		expectError    bool
		description    string
	}{
		{
			name:           "simple label check with has()",
			expression:     "has(self.metadata.labels.app)",
			expectedResult: true,
			expectError:    false,
			description:    "Verify has() works for checking label existence",
		},
		{
			name:           "label check with has() and value comparison",
			expression:     "has(self.metadata.labels.app) && self.metadata.labels.app == 'web'",
			expectedResult: true,
			expectError:    false,
			description:    "Verify has() + value check works",
		},
		{
			name:           "missing label with has() returns false",
			expression:     "has(self.metadata.labels.nonexistent)",
			expectedResult: false,
			expectError:    false,
			description:    "Verify has() returns false for missing labels",
		},
		{
			name:           "bracket notation for labels with dots",
			expression:     "has(self.metadata.labels['app.kubernetes.io/component']) && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
			expectedResult: true,
			expectError:    false,
			description:    "Verify bracket notation works for labels with special characters",
		},
		{
			name:           "multiple label checks with logical AND",
			expression:     "has(self.metadata.labels.app) && self.metadata.labels.app == 'web' && has(self.metadata.labels.tier) && self.metadata.labels.tier == 'frontend'",
			expectedResult: true,
			expectError:    false,
			description:    "Verify multiple label checks work",
		},
		{
			name:           "annotation check with has()",
			expression:     "has(self.metadata.annotations.description) && self.metadata.annotations.description == 'test annotation'",
			expectedResult: true,
			expectError:    false,
			description:    "Verify annotations work the same as labels",
		},
		{
			name:        "invalid .exists() syntax should fail compilation",
			expression:  "self.metadata.labels.exists(l, l.key == 'app')",
			expectError: true,
			description: "Verify .exists() syntax fails as expected since labels is a map, not a list",
		},
		{
			name:           "name check with startsWith()",
			expression:     "self.metadata.name.startsWith('test-')",
			expectedResult: true,
			expectError:    false,
			description:    "Verify string methods work on metadata fields",
		},
		{
			name:           "complex expression with OR logic",
			expression:     "(has(self.metadata.labels.app) && self.metadata.labels.app == 'web') || (has(self.metadata.labels.tier) && self.metadata.labels.tier == 'backend')",
			expectedResult: true,
			expectError:    false,
			description:    "Verify OR logic works correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.evaluate(tt.expression, pod)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: Expected error but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("%s: Unexpected error: %v", tt.description, err)
				return
			}

			if result != tt.expectedResult {
				t.Errorf("%s: Expected result %v, got %v", tt.description, tt.expectedResult, result)
			}
		})
	}
}

// TestCELExpressionValidation tests the validation function used at admission time
func TestCELExpressionValidation(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		expectError bool
		description string
	}{
		{
			name:        "valid map access expression",
			expression:  "has(self.metadata.labels.app) && self.metadata.labels.app == 'web'",
			expectError: false,
			description: "Valid expression should pass validation",
		},
		{
			name:        "valid bracket notation",
			expression:  "has(self.metadata.labels['app.kubernetes.io/component'])",
			expectError: false,
			description: "Bracket notation should be valid",
		},
		{
			name:        "invalid .exists() on map",
			expression:  "self.metadata.labels.exists(l, l.key == 'app')",
			expectError: true,
			description: "exists() method should fail on map type",
		},
		{
			name:        "incomplete expression",
			expression:  "self.metadata.name ==",
			expectError: true,
			description: "Incomplete expression should fail",
		},
		{
			name:        "non-boolean return",
			expression:  "self.metadata.name",
			expectError: true,
			description: "Non-boolean expression should fail",
		},
		{
			name:        "valid name check",
			expression:  "self.metadata.name.startsWith('postgres-')",
			expectError: false,
			description: "String method should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCELExpression(tt.expression)

			if tt.expectError && err == nil {
				t.Errorf("%s: Expected error but got none", tt.description)
			}

			if !tt.expectError && err != nil {
				t.Errorf("%s: Unexpected error: %v", tt.description, err)
			}
		})
	}
}
