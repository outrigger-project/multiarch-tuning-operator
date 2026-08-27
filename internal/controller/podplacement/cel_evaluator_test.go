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
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/api/common/plugins"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var _ = Describe("CEL Evaluator", func() {

	Describe("newCELEvaluator", func() {
		It("should create a valid CEL evaluator", func() {
			evaluator, err := newCELEvaluator()
			Expect(err).NotTo(HaveOccurred(), "Failed to create CEL evaluator")
			Expect(evaluator).NotTo(BeNil(), "CEL evaluator is nil")
			Expect(evaluator.env).NotTo(BeNil(), "CEL environment is nil")
			Expect(evaluator.cache).NotTo(BeNil(), "CEL cache is nil")
		})
	})

	Describe("CELEvaluator.compile", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred(), "Failed to create CEL evaluator")
		})

		DescribeTable("should compile expressions correctly",
			func(expression string, expectError bool) {
				_, err := evaluator.compile(expression)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry("valid boolean expression",
				"self.metadata.name == 'test-pod'", false),
			Entry("valid label check with has() and map access",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'web'", false),
			Entry("exists() expression compiles with DynType",
				"self.metadata.labels.exists(l, l.key == 'app' && l.value == 'web')", false),
			Entry("invalid syntax",
				"self.metadata.name ==", true),
			Entry("non-boolean return type",
				"self.metadata.name", true),
			Entry("valid label check with bracket notation",
				"'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database'", false),
			Entry("missing label check returns false safely",
				"has(self.metadata.labels.nonexistent) && self.metadata.labels.nonexistent == 'value'", false),
		)

		It("should return cached program on second compile", func() {
			expression := "self.metadata.name == 'test'"
			prog1, err := evaluator.compile(expression)
			Expect(err).NotTo(HaveOccurred(), "First compile failed")
			prog2, err := evaluator.compile(expression)
			Expect(err).NotTo(HaveOccurred(), "Second compile failed")
			Expect(prog1).To(BeIdenticalTo(prog2), "Expected cached program to be returned")
		})
	})

	Describe("CELEvaluator.evaluate", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("should evaluate expressions correctly",
			func(expression string, pod *corev1.Pod, expectedResult bool, expectError bool) {
				result, err := evaluator.evaluate(expression, pod)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(expectedResult))
				}
			},
			Entry("match by name",
				"self.metadata.name == 'nginx-pod'",
				NewPod().WithName("nginx-pod").Build(),
				true, false),
			Entry("no match by name",
				"self.metadata.name == 'nginx-pod'",
				NewPod().WithName("redis-pod").Build(),
				false, false),
			Entry("match by label",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'web'",
				NewPod().WithLabels("app", "web").Build(),
				true, false),
			Entry("name starts with",
				"self.metadata.name.startsWith('redis-')",
				NewPod().WithName("redis-master").Build(),
				true, false),
		)

		DescribeTable("negative cases",
			func(expression string, pod *corev1.Pod, expectError bool, description string) {
				_, err := evaluator.evaluate(expression, pod)
				if expectError {
					Expect(err).To(HaveOccurred(), description)
				} else {
					Expect(err).NotTo(HaveOccurred(), description)
				}
			},
			Entry("nil pod",
				"self.metadata.name == 'test'", nil, false,
				"Should handle nil pod gracefully"),
			Entry("empty expression",
				"", NewPod().WithName("test").Build(), true,
				"Should reject empty expression"),
			Entry("malformed CEL syntax",
				"self.metadata.name ==", NewPod().WithName("test").Build(), true,
				"Should reject malformed syntax"),
			Entry("undefined field access on DynType evaluator returns false, not error",
				"self.metadata.nonexistent == 'value'", NewPod().WithName("test").Build(), false,
				"DynType runtime evaluator: unknown metadata field access does not error"),
			Entry("type mismatch errors at runtime",
				"self.metadata.name + 123", NewPod().WithName("test").Build(), true,
				"Should detect type mismatches at runtime"),
			Entry("missing label key",
				"has(self.metadata.labels.nonexistent)", NewPod().WithName("test").Build(), false,
				"Should handle missing label keys with has()"),
			Entry("nil labels map",
				"has(self.metadata.labels.app)", NewPod().WithName("test").Build(), false,
				"Should handle nil labels map"),
			Entry("empty labels map",
				"has(self.metadata.labels.app)", NewPod().WithName("test").WithLabels().Build(), false,
				"Should handle empty labels map"),
			Entry("nil annotations map",
				"has(self.metadata.annotations.key)", NewPod().WithName("test").Build(), false,
				"Should handle nil annotations map"),
			Entry("special characters in name",
				"self.metadata.name == 'test-pod_123.example'", NewPod().WithName("test-pod_123.example").Build(), false,
				"Should handle special characters in names"),
			Entry("unicode in labels",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'тест'",
				NewPod().WithName("test").WithLabels("app", "тест").Build(),
				false, "Should handle unicode in label values"),
			Entry("very long expression",
				"self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test' && self.metadata.name == 'test'",
				NewPod().WithName("test").Build(), false,
				"Should handle long expressions"),
			Entry("complex boolean logic",
				"(self.metadata.name == 'test' || self.metadata.name == 'prod') && (has(self.metadata.labels.app) || has(self.metadata.labels.tier))",
				NewPod().WithName("test").WithLabels("app", "web").Build(),
				false, "Should handle complex boolean logic"),
		)

		It("should be thread-safe for concurrent evaluation", func() {
			expression := "self.metadata.name.startsWith('test-')"
			errs := make(chan error, 10)
			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					pod := NewPod().WithName("test-pod").Build()
					_, err := evaluator.evaluate(expression, pod)
					if err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				Expect(err).NotTo(HaveOccurred(), "Concurrent evaluation error")
			}
		})
	})

	Describe("CELEvaluator.evaluateRules", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with postgres and redis rules", func() {
			var rules []plugins.ArchitectureRule

			BeforeEach(func() {
				rules = []plugins.ArchitectureRule{
					NewRule("postgres-rule", "self.metadata.name.startsWith('postgres-')", "ppc64le"),
					NewRule("redis-rule", "self.metadata.name.startsWith('redis-')", "amd64", "ppc64le"),
				}
			})

			It("should match first rule for postgres-db", func() {
				pod := NewPod().WithName("postgres-db").Build()
				rr := evaluator.evaluateRules(rules, pod)
				Expect(rr.matched).To(BeTrue())
				Expect(rr.architectures).To(ConsistOf("ppc64le"))
				Expect(rr.ruleName).To(Equal("postgres-rule"))
			})

			It("should match second rule for redis-cache", func() {
				pod := NewPod().WithName("redis-cache").Build()
				rr := evaluator.evaluateRules(rules, pod)
				Expect(rr.matched).To(BeTrue())
				Expect(rr.architectures).To(ConsistOf("amd64", "ppc64le"))
				Expect(rr.ruleName).To(Equal("redis-rule"))
			})

			It("should not match for nginx-web", func() {
				pod := NewPod().WithName("nginx-web").Build()
				rr := evaluator.evaluateRules(rules, pod)
				Expect(rr.matched).To(BeFalse())
			})
		})

		It("should return allErrored=true when all rules are malformed", func() {
			malformedRules := []plugins.ArchitectureRule{
				NewRule("bad1", "self.metadata.name ==", "amd64"),
				NewRule("bad2", "invalid syntax !!!!", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			rr := evaluator.evaluateRules(malformedRules, pod)
			Expect(rr.allErrored).To(BeTrue(),
				"expected allErrored=true when all rules are malformed")
			Expect(rr.matched).To(BeFalse())
		})

		It("should return allErrored=false when some rules are valid (even if they don't match)", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("bad", "self.metadata.name ==", "amd64"),
				NewRule("valid-nomatch", "self.metadata.name == 'other'", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			rr := evaluator.evaluateRules(rules, pod)
			Expect(rr.allErrored).To(BeFalse(),
				"expected allErrored=false when at least one valid rule exists")
			Expect(rr.matched).To(BeFalse())
		})

		It("should evaluate subsequent rules when first is malformed and second is valid and matches", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("bad", "self.metadata.name ==", "amd64"),
				NewRule("good", "self.metadata.name == 'test-pod'", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			rr := evaluator.evaluateRules(rules, pod)
			Expect(rr.allErrored).To(BeFalse())
			Expect(rr.matched).To(BeTrue())
			Expect(rr.architectures).To(ConsistOf("ppc64le"))
		})
	})

	Describe("evaluateCELArchitecturePlacement", func() {
		DescribeTable("standard cases",
			func(rules []plugins.ArchitectureRule, fallback []string, pod *corev1.Pod,
				expectError bool, expectedArchs []string, expectedMatched bool) {
				result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
				if expectError {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.matched).To(Equal(expectedMatched))
				Expect(result.architectures).To(HaveLen(len(expectedArchs)))
				for i, arch := range expectedArchs {
					Expect(result.architectures[i]).To(Equal(arch))
				}
			},
			Entry("rule matches",
				[]plugins.ArchitectureRule{NewRule("test-rule", "self.metadata.name == 'test-pod'", "ppc64le")},
				[]string{"amd64"},
				NewPod().WithName("test-pod").Build(),
				false, []string{"ppc64le"}, true),
			Entry("no rule matches, use fallback",
				[]plugins.ArchitectureRule{NewRule("test-rule", "self.metadata.name == 'other-pod'", "ppc64le")},
				[]string{"amd64"},
				NewPod().WithName("test-pod").Build(),
				false, []string{"amd64"}, false),
			Entry("no rules, use fallback",
				[]plugins.ArchitectureRule{},
				[]string{"amd64", "ppc64le"},
				NewPod().WithName("test-pod").Build(),
				false, []string{"amd64", "ppc64le"}, false),
		)

		DescribeTable("edge cases",
			func(rules []plugins.ArchitectureRule, fallback []string, pod *corev1.Pod,
				expectError bool, expectedArchs []string, expectedMatched bool, description string) {
				result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
				if expectError {
					Expect(err).To(HaveOccurred(), description)
					return
				}
				Expect(err).NotTo(HaveOccurred(), description)
				Expect(result).NotTo(BeNil(), description)
				Expect(result.matched).To(Equal(expectedMatched), description)
				Expect(result.architectures).To(HaveLen(len(expectedArchs)), description)
				for i, arch := range expectedArchs {
					Expect(result.architectures[i]).To(Equal(arch), description)
				}
			},
			Entry("nil rules and nil fallback",
				nil, nil, NewPod().WithName("test").Build(),
				true, nil, false, "Should reject nil rules and fallback"),
			Entry("empty rules with fallback",
				[]plugins.ArchitectureRule{}, []string{"amd64"},
				NewPod().WithName("test").Build(),
				false, []string{"amd64"}, false, "Should use fallback with empty rules"),
			Entry("all rules fail to match",
				[]plugins.ArchitectureRule{
					NewRule("rule1", "self.metadata.name == 'nomatch1'", "ppc64le"),
					NewRule("rule2", "self.metadata.name == 'nomatch2'", "s390x"),
				},
				[]string{"amd64"},
				NewPod().WithName("test").Build(),
				false, []string{"amd64"}, false, "Should use fallback when no rules match"),
			Entry("first rule has invalid expression",
				[]plugins.ArchitectureRule{
					NewRule("invalid", "invalid syntax", "ppc64le"),
					NewRule("valid", "self.metadata.name == 'test'", "amd64"),
				},
				[]string{"s390x"},
				NewPod().WithName("test").Build(),
				false, []string{"amd64"}, true, "Should skip invalid rule and continue to next"),
			Entry("rule with empty architectures list",
				[]plugins.ArchitectureRule{NewRule("empty-arch", "self.metadata.name == 'test'")},
				[]string{"amd64"},
				NewPod().WithName("test").Build(),
				false, []string{}, true, "Should handle empty architectures list"),
			Entry("multiple architectures in single rule",
				[]plugins.ArchitectureRule{NewRule("multi-arch", "self.metadata.name == 'test'", "amd64", "arm64", "ppc64le", "s390x")},
				[]string{"amd64"},
				NewPod().WithName("test").Build(),
				false, []string{"amd64", "arm64", "ppc64le", "s390x"}, true, "Should handle multiple architectures"),
			Entry("pod with no metadata",
				[]plugins.ArchitectureRule{NewRule("rule1", "self.metadata.name == 'test'", "amd64")},
				[]string{"ppc64le"},
				NewPod().Build(),
				false, []string{"ppc64le"}, false, "Should handle pod with no metadata"),
			Entry("pod with empty name",
				[]plugins.ArchitectureRule{NewRule("rule1", "self.metadata.name == ''", "amd64")},
				[]string{"ppc64le"},
				NewPod().WithName("").Build(),
				false, []string{"amd64"}, true, "Should handle pod with empty name"),
		)

		It("should treat all-errored rules as non-matching and return fallback with allRulesErrored=true", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("bad", "self.metadata.name ==", "amd64"),
			}
			fallback := []string{"amd64"}
			pod := NewPod().WithName("test-pod").Build()
			result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.allRulesErrored).To(BeTrue())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(ConsistOf("amd64"))
		})

		It("should use fallback with empty rules and not set allErrored", func() {
			result, err := evaluateCELArchitecturePlacement(
				[]plugins.ArchitectureRule{},
				[]string{"amd64"},
				NewPod().WithName("test").Build(),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(ConsistOf("amd64"))
		})
	})

	Describe("validateCELExpression", func() {
		DescribeTable("should validate expressions",
			func(expression string, expectError bool) {
				err := validateCELExpression(expression)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry("valid expression", "self.metadata.name == 'test'", false),
			Entry("invalid syntax", "self.metadata.name ==", true),
			Entry("non-boolean return", "self.metadata.name", true),
			Entry("valid map access expression",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'web'", false),
			Entry("valid bracket notation",
				"'app.kubernetes.io/component' in self.metadata.labels", false),
			Entry("exists() on dynamic map",
				"self.metadata.labels.exists(l, l.key == 'app')", false),
			Entry("incomplete expression",
				"self.metadata.name ==", true),
			Entry("valid name check",
				"self.metadata.name.startsWith('postgres-')", false),
		)
	})

	Describe("CEL map access syntax", func() {
		var evaluator *celEvaluator
		var pod *corev1.Pod

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
			pod = NewPod().
				WithName("test-pod").
				WithNamespace("test-ns").
				WithLabels(
					"app", "web",
					"tier", "frontend",
					"app.kubernetes.io/component", "database",
					"app.kubernetes.io/part-of", "wordpress",
				).
				WithAnnotations(map[string]string{
					"description": "test annotation",
				}).
				Build()
		})

		DescribeTable("should handle map access correctly",
			func(expression string, expectedResult bool, expectError bool, description string) {
				result, err := evaluator.evaluate(expression, pod)
				if expectError {
					Expect(err).To(HaveOccurred(), description)
					return
				}
				Expect(err).NotTo(HaveOccurred(), description)
				Expect(result).To(Equal(expectedResult), description)
			},
			Entry("simple label check with has()",
				"has(self.metadata.labels.app)", true, false,
				"Verify has() works for checking label existence"),
			Entry("label check with has() and value comparison",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'web'", true, false,
				"Verify has() + value check works"),
			Entry("missing label with has() returns false",
				"has(self.metadata.labels.nonexistent)", false, false,
				"Verify has() returns false for missing labels"),
			Entry("bracket notation for labels with dots",
				"'app.kubernetes.io/component' in self.metadata.labels && self.metadata.labels['app.kubernetes.io/component'] == 'database'",
				true, false, "Verify bracket notation works for labels with special characters"),
			Entry("multiple label checks with logical AND",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'web' && has(self.metadata.labels.tier) && self.metadata.labels.tier == 'frontend'",
				true, false, "Verify multiple label checks work"),
			Entry("annotation check with has()",
				"has(self.metadata.annotations.description) && self.metadata.annotations.description == 'test annotation'",
				true, false, "Verify annotations work the same as labels"),
			Entry(".exists() on labels compiles with DynType",
				"self.metadata.labels.exists(l, l.key == 'app')", false, false,
				"DynType runtime evaluator: .exists() on a dynamic map compiles without error"),
			Entry("name check with startsWith()",
				"self.metadata.name.startsWith('test-')", true, false,
				"Verify string methods work on metadata fields"),
			Entry("complex expression with OR logic",
				"(has(self.metadata.labels.app) && self.metadata.labels.app == 'web') || (has(self.metadata.labels.tier) && self.metadata.labels.tier == 'backend')",
				true, false, "Verify OR logic works correctly"),
			// Index / bracket notation on fields not exposed by podToMap.
			// self is the map returned by podToMap, which only has a "metadata" key.
			// DynType: bracket access compiles; absent top-level key produces "no such key"
			// at runtime, which evaluateWithMap converts to false (non-error).
			Entry("index on self for absent top-level key evaluates to false",
				"self['spec'] == 'anything'", false, false,
				"DynType: self['spec'] bracket access on absent key evaluates to false, not error"),
			// self.spec does not exist in podToMap; chained bracket access on a missing
			// intermediate key also produces "no such key" -> false at runtime.
			Entry("chained bracket access through absent spec field evaluates to false",
				"self.spec['nodeName'] == 'node-1'", false, false,
				"DynType: self.spec['nodeName'] on absent spec key evaluates to false, not error"),
			// self.metadata.labels exists, but the label key 'spec' is absent.
			// Bracket access on an existing map with a missing key produces "no such key" -> false.
			Entry("bracket access on labels map for absent key evaluates to false",
				"self.metadata.labels['spec'] == 'value'", false, false,
				"DynType: self.metadata.labels['spec'] for absent label key evaluates to false, not error"),
		)

		DescribeTable("standalone index expressions are rejected at compile time",
			// A bare bracket/index expression such as self['spec'] returns dyn, not bool.
			// The compile() function rejects non-boolean output types, so expressions like
			// these must never reach the evaluator.  Users must always wrap index access
			// in a boolean comparison (e.g. self['spec'] == 'value') or an 'in' check.
			func(expression string, description string) {
				_, err := evaluator.compile(expression)
				Expect(err).To(HaveOccurred(), description)
				Expect(err.Error()).To(ContainSubstring("boolean"), description)
			},
			Entry("bare self['spec'] is non-boolean",
				"self['spec']",
				"bare bracket access on self returns dyn, not bool -- must be rejected"),
			Entry("bare self.spec['nodeName'] is non-boolean",
				"self.spec['nodeName']",
				"bare bracket access through absent field returns dyn, not bool -- must be rejected"),
			Entry("bare self.metadata.labels['spec'] is non-boolean",
				"self.metadata.labels['spec']",
				"bare bracket label access returns dyn, not bool -- must be rejected"),
		)
	})

	Describe("real-world CEL expression scenarios", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("should match production scenarios",
			func(expression string, pod *corev1.Pod, expectedResult bool, description string) {
				result, err := evaluator.evaluate(expression, pod)
				Expect(err).NotTo(HaveOccurred(), description)
				Expect(result).To(Equal(expectedResult), description)
			},
			Entry("operator namespace - openshift-operators",
				"self.metadata.namespace == 'openshift-operators'",
				NewPod().WithName("operator-pod").WithNamespace("openshift-operators").Build(),
				true, "Should match pods in openshift-operators namespace"),
			Entry("well-known label - app component",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'database'",
				NewPod().WithName("db-pod").WithLabels("app", "database").Build(),
				true, "Should match app label"),
			Entry("well-known label - component",
				"has(self.metadata.labels.component) && self.metadata.labels.component == 'postgresql'",
				NewPod().WithName("postgres-pod").WithLabels("component", "postgresql").Build(),
				true, "Should match component label"),
			Entry("combined labels - app and component",
				"has(self.metadata.labels.app) && self.metadata.labels.app == 'database' && has(self.metadata.labels.component) && self.metadata.labels.component == 'postgresql'",
				NewPod().WithName("postgres-db").WithLabels("app", "database", "component", "postgresql").Build(),
				true, "Should match multiple labels combined"),
			Entry("tier and environment labels",
				"has(self.metadata.labels.tier) && self.metadata.labels.tier == 'frontend' && has(self.metadata.labels.environment) && self.metadata.labels.environment == 'production'",
				NewPod().WithName("frontend-prod").WithLabels("tier", "frontend", "environment", "production").Build(),
				true, "Should match tier and environment labels"),
			Entry("priority label - critical",
				"has(self.metadata.labels.priority) && self.metadata.labels.priority == 'critical'",
				NewPod().WithName("critical-service").WithLabels("priority", "critical").Build(),
				true, "Should match priority label"),
			Entry("OR condition - backend with gold SLA",
				"has(self.metadata.labels.priority) && self.metadata.labels.priority == 'critical' || (has(self.metadata.labels.tier) && self.metadata.labels.tier == 'backend' && has(self.metadata.labels.sla) && self.metadata.labels.sla == 'gold')",
				NewPod().WithName("backend-gold").WithLabels("tier", "backend", "sla", "gold").Build(),
				true, "Should match OR condition with multiple labels"),
			Entry("name prefix - redis pods",
				"self.metadata.name.startsWith('redis-')",
				NewPod().WithName("redis-master-0").Build(),
				true, "Should match name prefix for StatefulSet pods"),
			Entry("name contains pattern",
				"self.metadata.name.contains('database')",
				NewPod().WithName("my-database-pod-123").Build(),
				true, "Should match name containing pattern"),
			Entry("namespace prefix match",
				"self.metadata.namespace.startsWith('prod-')",
				NewPod().WithName("app-pod").WithNamespace("prod-apps").Build(),
				true, "Should match namespace prefix"),
			Entry("label exists check only",
				"has(self.metadata.labels.migrationReady)",
				NewPod().WithName("migrating-pod").WithLabels("migrationReady", "true").Build(),
				true, "Should check if label exists"),
			Entry("label does not exist",
				"!has(self.metadata.labels.legacy)",
				NewPod().WithName("modern-pod").WithLabels().Build(),
				true, "Should match when label does not exist"),
		)
	})

	Describe("podToMap", func() {
		DescribeTable("should expose correct metadata fields",
			func(pod *corev1.Pod, wantMeta map[string]interface{}) {
				m := podToMap(pod)
				meta, ok := m["metadata"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "metadata is not map[string]interface{}")
				for key, want := range wantMeta {
					got, exists := meta[key]
					Expect(exists).To(BeTrue(), "metadata[%q] missing", key)
					wantMap, wantIsMap := want.(map[string]interface{})
					gotMap, gotIsMap := got.(map[string]interface{})
					if wantIsMap && gotIsMap {
						Expect(len(gotMap)).To(Equal(len(wantMap)), "metadata[%q] map length mismatch", key)
					} else {
						Expect(got).To(Equal(want), "metadata[%q] value mismatch", key)
					}
				}
			},
			Entry("nil pod returns empty metadata with generateName",
				nil,
				map[string]interface{}{
					"name": "", "generateName": "", "namespace": "",
					"labels": map[string]interface{}{}, "annotations": map[string]interface{}{},
				}),
			Entry("pod with generateName",
				NewPod().WithGenerateName("worker-").WithNamespace("default").Build(),
				map[string]interface{}{
					"name": "", "generateName": "worker-", "namespace": "default",
					"labels": map[string]interface{}{}, "annotations": map[string]interface{}{},
				}),
			Entry("pod with name and no generateName",
				NewPod().WithName("my-pod").WithNamespace("ns").Build(),
				map[string]interface{}{
					"name": "my-pod", "generateName": "", "namespace": "ns",
					"labels": map[string]interface{}{}, "annotations": map[string]interface{}{},
				}),
		)
	})

	Describe("generateName in CEL expressions", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("should match generateName correctly",
			func(expression string, pod *corev1.Pod, expectedResult bool) {
				result, err := evaluator.evaluate(expression, pod)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(expectedResult))
			},
			Entry("match by generateName prefix",
				"self.metadata.generateName.startsWith('worker-')",
				NewPod().WithGenerateName("worker-").Build(),
				true),
			Entry("no match when generateName differs",
				"self.metadata.generateName.startsWith('worker-')",
				NewPod().WithGenerateName("redis-").Build(),
				false),
			Entry("empty generateName does not match prefix",
				"self.metadata.generateName.startsWith('worker-')",
				NewPod().WithName("explicit-name").Build(),
				false),
		)
	})

	Describe("first-match-wins and priority ordering", func() {
		It("should evaluate rules strictly in order and stop at first match", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("first-rule", "self.metadata.name.startsWith('test-')", "ppc64le"),
				NewRule("second-rule-also-matches", "self.metadata.name.startsWith('test-')", "amd64"),
				NewRule("third-rule", "self.metadata.name == 'test-pod'", "arm64"),
			}
			pod := NewPod().WithName("test-pod").Build()
			result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeTrue())
			Expect(result.ruleName).To(Equal("first-rule"))
			Expect(result.architectures).To(ConsistOf("ppc64le"))
		})

		It("should not apply fallback when a rule matches", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("matching-rule", "self.metadata.name == 'test-pod'", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			fallback := []string{"amd64", "arm64"}
			result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeTrue())
			Expect(result.architectures).To(ConsistOf("ppc64le"))
		})

		It("should only apply first matching rule when multiple rules match", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("broad-match", "has(self.metadata.labels.app)", "ppc64le"),
				NewRule("specific-match", "self.metadata.labels.app == 'web'", "amd64"),
			}
			pod := NewPod().WithLabels("app", "web").Build()
			result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ruleName).To(Equal("broad-match"))
			Expect(result.architectures).To(ConsistOf("ppc64le"))
		})
	})

	Describe("invalid CEL expression handling", func() {
		It("should not panic on invalid CEL expressions", func() {
			Expect(func() {
				rules := []plugins.ArchitectureRule{
					NewRule("invalid-syntax", "self.metadata.name ==", "ppc64le"),
					NewRule("valid-rule", "self.metadata.name == 'test-pod'", "amd64"),
				}
				pod := NewPod().WithName("test-pod").Build()
				result, err := evaluateCELArchitecturePlacement(rules, []string{"s390x"}, pod)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.matched).To(BeTrue())
				Expect(result.ruleName).To(Equal("valid-rule"))
			}).NotTo(Panic())
		})

		It("should treat invalid CEL as false (non-matching) and use fallback", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("invalid-expression", "self.nonexistent.field.access", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			result, err := evaluateCELArchitecturePlacement(rules, []string{"amd64"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(ConsistOf("amd64"))
		})

		It("should use fallback when all rules are invalid", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("invalid-1", "self.metadata.name ==", "ppc64le"),
				NewRule("invalid-2", "self.nonexistent.field", "arm64"),
			}
			pod := NewPod().WithName("test-pod").Build()
			result, err := evaluateCELArchitecturePlacement(rules, []string{"amd64", "s390x"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(HaveLen(2))
		})

		It("should remain stable across repeated evaluations of invalid CEL", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("invalid-rule", "self.metadata.name ==", "ppc64le"),
			}
			pod := NewPod().WithName("test-pod").Build()
			for i := 0; i < 10; i++ {
				result, err := evaluateCELArchitecturePlacement(rules, []string{"amd64"}, pod)
				Expect(err).NotTo(HaveOccurred(), "Iteration %d", i)
				Expect(result.matched).To(BeFalse(), "Iteration %d", i)
				Expect(result.architectures).To(ConsistOf("amd64"), "Iteration %d", i)
			}
		})
	})

	Describe("concurrent CEL compilation and cache reuse", func() {
		var evaluator *celEvaluator

		BeforeEach(func() {
			var err error
			evaluator, err = newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be thread-safe for concurrent compilation", func() {
			expressions := []string{
				"self.metadata.name == 'test-1'",
				"self.metadata.name == 'test-2'",
				"self.metadata.name == 'test-3'",
				"self.metadata.name.startsWith('test-')",
				"has(self.metadata.labels.app)",
			}
			var wg sync.WaitGroup
			errors := make(chan error, len(expressions)*10)
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
			for err := range errors {
				Expect(err).NotTo(HaveOccurred(), "Concurrent compilation error")
			}
		})

		It("should reuse cached compiled expressions", func() {
			expression := "self.metadata.name == 'test'"
			prog1, err := evaluator.compile(expression)
			Expect(err).NotTo(HaveOccurred())
			prog2, err := evaluator.compile(expression)
			Expect(err).NotTo(HaveOccurred())
			Expect(prog1).To(BeIdenticalTo(prog2), "Expected cached program to be reused")

			evaluator.mu.Lock()
			found := evaluator.cache.Contains(expression)
			evaluator.mu.Unlock()
			Expect(found).To(BeTrue(), "Expression not found in cache")
		})
	})

	Describe("nil and empty pod handling", func() {
		It("should handle nil pod without panicking or erroring", func() {
			rules := []plugins.ArchitectureRule{
				NewRule("test-rule", "self.metadata.name == 'test'", "amd64"),
			}
			result, err := evaluateCELArchitecturePlacement(rules, []string{"ppc64le"}, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(ConsistOf("ppc64le"))
		})

		It("should not modify pod for empty architectures list", func() {
			pod := NewPod().WithName("test-pod").Build()
			modified := applyArchitectureConstraints(pod, []string{})
			Expect(modified).To(BeFalse())
			Expect(pod.Spec.Affinity).To(BeNil())
		})

		It("should use fallback for empty rules list", func() {
			rules := []plugins.ArchitectureRule{}
			pod := NewPod().WithName("test-pod").Build()
			result, err := evaluateCELArchitecturePlacement(rules, []string{"amd64", "arm64"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(HaveLen(2))
		})
	})

	Describe("applyCELInWebhook -- malformed CEL and fallback interaction", func() {
		var ctx context.Context
		var recorder *record.FakeRecorder

		BeforeEach(func() {
			ctx = context.Background()
			recorder = record.NewFakeRecorder(8)
		})

		It("should skip malformed high-priority PPC and apply valid lower-priority PPC", func() {
			pod := NewPod().WithName("my-workload").WithNamespace("default").
				WithContainersImages("nginx:latest").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("high-prio-malformed").
					WithNamespace("default").
					WithPriority(200).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("malformed", "self.metadata.name ==", "amd64"),
						}).
					Build(),
				*NewPodPlacementConfig().
					WithName("low-prio-valid").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"s390x"},
						[]plugins.ArchitectureRule{
							NewRule("match-by-name", "self.metadata.name == 'my-workload'", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)

			Expect(wrappedPod.Spec.Affinity).NotTo(BeNil())
			Expect(wrappedPod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			Expect(wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution).NotTo(BeNil())
			var foundArchs []string
			for _, term := range wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == utils.ArchLabel {
						foundArchs = append(foundArchs, expr.Values...)
					}
				}
			}
			Expect(foundArchs).To(ConsistOf("ppc64le"),
				"If amd64 appears here, the malformed high-priority PPC's fallback was incorrectly applied")
		})

		It("should apply fallback when CEL expression evaluates to false (valid but non-matching)", func() {
			pod := NewPod().WithName("unmatched-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("ppc-no-match").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"arm64"},
						[]plugins.ArchitectureRule{
							NewRule("no-match", "self.metadata.name == 'other-pod'", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			var foundArchs []string
			if wrappedPod.Spec.Affinity != nil && wrappedPod.Spec.Affinity.NodeAffinity != nil &&
				wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				for _, term := range wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel {
							foundArchs = append(foundArchs, expr.Values...)
						}
					}
				}
			}
			Expect(foundArchs).To(ConsistOf("arm64"))
		})

		It("should apply rule architectures when CEL expression evaluates to true", func() {
			pod := NewPod().WithName("matched-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("ppc-match").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("match", "self.metadata.name == 'matched-pod'", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			var foundArchs []string
			if wrappedPod.Spec.Affinity != nil && wrappedPod.Spec.Affinity.NodeAffinity != nil &&
				wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				for _, term := range wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel {
							foundArchs = append(foundArchs, expr.Values...)
						}
					}
				}
			}
			Expect(foundArchs).To(ConsistOf("ppc64le"))
		})

		It("should evaluate lower-priority PPC when first has all-errored CEL", func() {
			pod := NewPod().WithName("app-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("p300-malformed").
					WithNamespace("default").
					WithPriority(200).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("bad", "!!! invalid", "amd64"),
						}).
					Build(),
				*NewPodPlacementConfig().
					WithName("p100-valid").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"arm64"},
						[]plugins.ArchitectureRule{
							NewRule("match", "self.metadata.name == 'app-pod'", "s390x"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			var foundArchs []string
			if wrappedPod.Spec.Affinity != nil && wrappedPod.Spec.Affinity.NodeAffinity != nil &&
				wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
				for _, term := range wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
					for _, expr := range term.MatchExpressions {
						if expr.Key == utils.ArchLabel {
							foundArchs = append(foundArchs, expr.Values...)
						}
					}
				}
			}
			Expect(foundArchs).To(ConsistOf("s390x"))
		})

		It("should not set arch constraint when all CEL rules are malformed and no PPC claims the pod", func() {
			pod := NewPod().WithName("no-match-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("all-malformed").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("bad", "self.metadata.name ==", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			if wrappedPod.Spec.Affinity != nil {
				if na := wrappedPod.Spec.Affinity.NodeAffinity; na != nil {
					if req := na.RequiredDuringSchedulingIgnoredDuringExecution; req != nil {
						Expect(req.NodeSelectorTerms).To(BeEmpty(),
							"expected no arch constraint when all CEL rules are malformed")
					}
				}
			}
		})
	})

	Describe("NodeAffinityLabel after applyCELInWebhook", func() {
		var ctx context.Context
		var recorder *record.FakeRecorder

		BeforeEach(func() {
			ctx = context.Background()
			recorder = record.NewFakeRecorder(8)
		})

		It("should set NodeAffinityLabel to 'overriden' when CEL successfully applies constraints", func() {
			pod := NewPod().WithName("target-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("ppc-cel-match").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("match", "self.metadata.name == 'target-pod'", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			Expect(wrappedPod.Labels[utils.NodeAffinityLabel]).To(Equal(utils.NodeAffinityLabelValueOverriden))
		})

		It("should NOT set NodeAffinityLabel to 'overriden' when all CEL rules fail", func() {
			pod := NewPod().WithName("error-pod").WithNamespace("default").
				WithLabels(utils.NodeAffinityLabel, utils.LabelValueNotSet).Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("ppc-malformed").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("bad", "self.metadata.name ==", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			Expect(wrappedPod.Labels[utils.NodeAffinityLabel]).NotTo(Equal(utils.NodeAffinityLabelValueOverriden))
		})

		It("should set NodeAffinityLabel to 'overriden' when CEL fallback is applied", func() {
			pod := NewPod().WithName("nomatch-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("ppc-fallback").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"s390x"},
						[]plugins.ArchitectureRule{
							NewRule("no-match", "self.metadata.name == 'other'", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			Expect(wrappedPod.Labels[utils.NodeAffinityLabel]).To(Equal(utils.NodeAffinityLabelValueOverriden))
		})
	})

	Describe("applyCELArchitecturePlacement -- controller path", func() {
		// Both webhook and controller skip PPCs when all CEL rules fail:
		//   malformed CEL -> allRulesErrored=true -> skip PPC, do not apply fallback

		// -- disabled-plugin tests (mirrors cel_new_critical_tests_test.go webhook cases) --

		It("should return false and not modify the pod when the CEL plugin is disabled (Enabled: false)", func() {
			// Verify the guard at cel_integration.go: !ppc.PluginsEnabled(...)
			recorder := record.NewFakeRecorder(8)
			reconciler := &PodReconciler{Recorder: recorder}

			ppc := *NewPodPlacementConfig().
				WithName("disabled-ppc").
				WithNamespace("default").
				WithPriority(100).
				WithCelArchitecturePlacement(false, []string{"ppc64le"},
					[]plugins.ArchitectureRule{
						NewRule("always-true", "true", "ppc64le"),
					}).
				Build()

			pod := NewPod().WithName("workload").WithNamespace("default").
				WithContainersImages("nginx:latest").Build()
			wrappedPod := newPod(pod, context.Background(), recorder)

			handled := reconciler.applyCELArchitecturePlacement(context.Background(), ppc, wrappedPod)

			Expect(handled).To(BeFalse(),
				"controller must return false when CEL plugin is disabled")
			Expect(wrappedPod.Spec.Affinity).To(BeNil(),
				"controller must not set affinity when CEL plugin is disabled")
		})

		It("should not add architecture NodeAffinity when plugin is disabled", func() {
			// No architecture constraint must be added; existing unrelated affinity unchanged.
			recorder := record.NewFakeRecorder(8)
			reconciler := &PodReconciler{Recorder: recorder}

			pod := NewPod().
				WithName("pod-with-affinity").
				WithNamespace("default").
				WithNodeSelectorTermsMatchExpressions(
					[]corev1.NodeSelectorRequirement{
						{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
					},
				).
				Build()

			ppc := *NewPodPlacementConfig().
				WithName("disabled-ppc").
				WithNamespace("default").
				WithCelArchitecturePlacement(false, []string{"ppc64le"}, nil).
				Build()

			wrappedPod := newPod(pod, context.Background(), recorder)
			handled := reconciler.applyCELArchitecturePlacement(context.Background(), ppc, wrappedPod)

			Expect(handled).To(BeFalse(),
				"controller must return false when CEL plugin is disabled")
			// Existing affinity must be completely unchanged.
			Expect(wrappedPod.Spec.Affinity).NotTo(BeNil(),
				"existing affinity must not be removed by a disabled CEL plugin")
			terms := wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1),
				"NodeSelectorTerms count must be unchanged when plugin is disabled")
			Expect(terms[0].MatchExpressions[0].Key).To(Equal("topology.kubernetes.io/zone"),
				"existing affinity term was modified by a disabled CEL plugin")
			// No arch label must have been injected.
			for _, expr := range terms[0].MatchExpressions {
				Expect(expr.Key).NotTo(Equal(utils.ArchLabel),
					"controller must not inject arch constraint when plugin is disabled")
			}
		})

		It("should not modify unrelated pod fields when plugin is disabled", func() {
			// Labels, annotations, nodeSelector non-arch keys must survive unchanged.
			recorder := record.NewFakeRecorder(8)
			reconciler := &PodReconciler{Recorder: recorder}

			pod := NewPod().
				WithName("unrelated-fields-pod").
				WithNamespace("default").
				WithLabels("app", "database", "tier", "backend").
				WithAnnotations(map[string]string{
					"custom-annotation": "custom-value",
				}).
				WithNodeSelectors("zone", "us-east-1").
				Build()

			ppc := *NewPodPlacementConfig().
				WithName("disabled-ppc").
				WithNamespace("default").
				WithCelArchitecturePlacement(false, []string{"ppc64le"}, nil).
				Build()

			wrappedPod := newPod(pod, context.Background(), recorder)
			handled := reconciler.applyCELArchitecturePlacement(context.Background(), ppc, wrappedPod)

			Expect(handled).To(BeFalse())
			Expect(wrappedPod.Labels["app"]).To(Equal("database"),
				"label 'app' must be unchanged when plugin is disabled")
			Expect(wrappedPod.Labels["tier"]).To(Equal("backend"),
				"label 'tier' must be unchanged when plugin is disabled")
			Expect(wrappedPod.Annotations["custom-annotation"]).To(Equal("custom-value"),
				"annotation must be unchanged when plugin is disabled")
			Expect(wrappedPod.Spec.NodeSelector["zone"]).To(Equal("us-east-1"),
				"non-arch nodeSelector key 'zone' must be unchanged when plugin is disabled")
			Expect(wrappedPod.Spec.Affinity).To(BeNil(),
				"no affinity must be set when plugin is disabled")
		})

		// -- malformed-CEL tests --

		It("should skip PPC and return false when all CEL rules are malformed", func() {
			// Construct a minimal PodReconciler; only the Recorder field is needed
			// because applyCELArchitecturePlacement does not call the API server.
			recorder := record.NewFakeRecorder(8)
			reconciler := &PodReconciler{Recorder: recorder}

			ppc := *NewPodPlacementConfig().
				WithName("malformed-ppc").
				WithNamespace("default").
				WithCelArchitecturePlacement(true, []string{"amd64"},
					[]plugins.ArchitectureRule{
						NewRule("bad", "self.metadata.name ==", "ppc64le"),
					}).
				Build()

			pod := NewPod().WithName("workload").WithNamespace("default").Build()
			wrappedPod := newPod(pod, context.Background(), recorder)

			handled := reconciler.applyCELArchitecturePlacement(context.Background(), ppc, wrappedPod)

			// The controller must NOT apply the fallback; skip the malformed PPC.
			Expect(handled).To(BeFalse(), "controller must skip malformed PPC without applying fallback")
			// Pod should have no affinity set by this PPC.
			if wrappedPod.Spec.Affinity != nil && wrappedPod.Spec.Affinity.NodeAffinity != nil {
				req := wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
				if req != nil {
					Expect(req.NodeSelectorTerms).To(BeEmpty(),
						"controller must not set NodeAffinity when all CEL rules are malformed")
				}
			}
		})

		It("should skip malformed PPC in both webhook and controller", func() {
			// Both code paths uniformly skip PPCs with all-malformed CEL rules.
			recorder := record.NewFakeRecorder(8)
			pod := NewPod().WithName("workload").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().
					WithName("malformed-ppc").
					WithNamespace("default").
					WithPriority(100).
					WithCelArchitecturePlacement(true, []string{"amd64"},
						[]plugins.ArchitectureRule{
							NewRule("bad", "self.metadata.name ==", "ppc64le"),
						}).
					Build(),
			}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, context.Background(), recorder)
			wh.applyCELInWebhook(context.Background(), wrappedPod, ppcs)

			// Webhook must NOT have written any NodeAffinity for the malformed PPC.
			if wrappedPod.Spec.Affinity != nil && wrappedPod.Spec.Affinity.NodeAffinity != nil {
				req := wrappedPod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
				if req != nil {
					Expect(req.NodeSelectorTerms).To(BeEmpty(),
						"webhook must not set NodeAffinity when the only PPC has all-malformed CEL rules")
				}
			}
		})
	})
	Context("Reliability", func() {

		// TestIdempotentRepeatedReconcile
		It("should produce stable pod state when repeatedly applying the same architectures (idempotent)", func() {
			pod := NewPod().WithName("test-pod").
				WithNodeSelectors(
					utils.ArchLabel, "amd64",
					"other-label", "value",
				).Build()
			architectures := []string{"ppc64le", "arm64"}
			for i := 0; i < 5; i++ {
				applyArchitectureConstraints(pod, architectures)

				if pod.Spec.NodeSelector != nil {
					Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel),
						"Iteration %d: Architecture still in nodeSelector", i)
					Expect(pod.Spec.NodeSelector["other-label"]).To(Equal("value"),
						"Iteration %d: Other label was modified", i)
				}

				Expect(pod.Spec.Affinity).NotTo(BeNil(), "Iteration %d: Node affinity missing", i)
				Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil(), "Iteration %d: Node affinity missing", i)

				terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
				Expect(terms).To(HaveLen(1), "Iteration %d: Expected 1 term (idempotent)", i)
				Expect(terms[0].MatchExpressions).To(HaveLen(1), "Iteration %d", i)
				Expect(terms[0].MatchExpressions[0].Key).To(Equal(utils.ArchLabel), "Iteration %d", i)
				Expect(terms[0].MatchExpressions[0].Values).To(HaveLen(2), "Iteration %d", i)
			}
		})

		// TestArchitectureConstraintsReplacedInPlaceOnRepeatedApply
		It("should replace architecture constraints in-place on repeated apply", func() {
			pod := NewPod().WithName("test-pod").Build()

			applyArchitectureConstraints(pod, []string{"amd64"})
			Expect(pod.Spec.Affinity).NotTo(BeNil())
			terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "After first application")
			Expect(terms[0].MatchExpressions[0].Values[0]).To(Equal("amd64"))

			applyArchitectureConstraints(pod, []string{"ppc64le"})
			terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "After second application (in-place replacement)")
			Expect(terms[0].MatchExpressions[0].Values[0]).To(Equal("ppc64le"))

			applyArchitectureConstraints(pod, []string{"arm64", "s390x"})
			terms = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			Expect(terms).To(HaveLen(1), "After third application (in-place replacement)")
			Expect(terms[0].MatchExpressions).To(HaveLen(1))
			Expect(terms[0].MatchExpressions[0].Values).To(HaveLen(2))
		})

		// TestNodeSelectorCleanupStableAcrossMultipleReconciles
		It("should keep nodeSelector cleanup stable across multiple reconciles", func() {
			pod := NewPod().WithName("test-pod").
				WithNodeSelectors(
					utils.ArchLabel, "amd64",
					"zone", "us-east-1",
					"tier", "frontend",
				).Build()

			for i := 0; i < 5; i++ {
				removed := removeArchitectureFromNodeSelector(pod)
				if i == 0 {
					Expect(removed).To(BeTrue(), "First cleanup should have removed architecture")
				} else {
					Expect(removed).To(BeFalse(), "Iteration %d: Cleanup should be idempotent", i)
				}
				Expect(pod.Spec.NodeSelector["zone"]).To(Equal("us-east-1"), "Iteration %d: zone label was modified", i)
				Expect(pod.Spec.NodeSelector["tier"]).To(Equal("frontend"), "Iteration %d: tier label was modified", i)
				Expect(pod.Spec.NodeSelector).NotTo(HaveKey(utils.ArchLabel), "Iteration %d: Architecture label still exists", i)
			}
		})

		// TestFallbackApplicationStable
		It("should keep fallback application stable across repeated runs", func() {
			rules := []plugins.ArchitectureRule{}
			fallback := []string{"amd64", "ppc64le"}
			pod := NewPod().WithName("test-pod").Build()

			for i := 0; i < 5; i++ {
				result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
				Expect(err).NotTo(HaveOccurred(), "Iteration %d", i)
				Expect(result.matched).To(BeFalse(), "Iteration %d", i)
				Expect(result.architectures).To(HaveLen(2), "Iteration %d", i)
			}
		})

		// TestNilAffinityHandling
		It("should handle nil affinity structures safely", func() {
			pod := NewPod().WithName("test-pod").Build()
			removed := removeArchitectureFromNodeAffinity(pod)
			Expect(removed).To(BeFalse(), "Should not report removal when affinity is nil")

			applyArchitectureNodeAffinity(pod, []string{"amd64"})
			Expect(pod.Spec.Affinity).NotTo(BeNil(), "Affinity should have been created")
		})
	})

	Context("Performance", func() {
		It("should handle concurrent cache access at capacity without errors", func() {
			evaluator, err := newCELEvaluator()
			Expect(err).NotTo(HaveOccurred())

			var wg sync.WaitGroup
			const goroutines = 50
			wg.Add(goroutines)
			for i := 0; i < goroutines; i++ {
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()
					expr := fmt.Sprintf("self.metadata.name == 'stress-test-%d'", idx)
					prog, compileErr := evaluator.compile(expr)
					Expect(compileErr).NotTo(HaveOccurred())
					Expect(prog).NotTo(BeNil())
				}(i)
			}
			wg.Wait()
		})

		It("should validate 500 CEL rules within acceptable latency", func() {
			plugin := &plugins.CelArchitecturePlacement{
				BasePlugin:            plugins.BasePlugin{Enabled: true},
				FallbackArchitectures: []string{utils.ArchitectureAmd64},
			}
			for i := 0; i < 500; i++ {
				plugin.Rules = append(plugin.Rules, NewRule(
					fmt.Sprintf("rule-%d", i),
					fmt.Sprintf("self.metadata.name == 'target-%d'", i),
					utils.ArchitecturePpc64le,
				))
			}

			start := time.Now()
			err := plugin.ValidateCELExpressions()
			elapsed := time.Since(start)

			Expect(err).NotTo(HaveOccurred())
			Expect(elapsed).To(BeNumerically("<", 5*time.Second),
				"500 rules should validate in under 5 seconds, took %v", elapsed)
		})

		It("should evaluate 1000 rules within acceptable latency", func() {
			rules := make([]plugins.ArchitectureRule, 1000)
			for i := 0; i < 1000; i++ {
				rules[i] = NewRule(
					fmt.Sprintf("rule-%d", i),
					fmt.Sprintf("self.metadata.name == 'target-%d'", i),
					utils.ArchitecturePpc64le,
				)
			}
			fallback := []string{utils.ArchitectureAmd64}
			pod := NewPod().WithName("no-match-pod").WithNamespace("default").Build()

			start := time.Now()
			result, err := evaluateCELArchitecturePlacement(rules, fallback, pod)
			elapsed := time.Since(start)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse(), "no rule should match")
			Expect(result.architectures).To(ConsistOf(utils.ArchitectureAmd64), "should use fallback")
			Expect(elapsed).To(BeNumerically("<", 2*time.Second),
				"1000 rules should evaluate in under 2 seconds, took %v", elapsed)
		})
	})

	Context("Nil Plugin Guard", func() {
		It("should return false without panicking when CelArchitecturePlacement is nil in the controller path", func() {
			recorder := record.NewFakeRecorder(8)
			reconciler := &PodReconciler{Recorder: recorder}

			ppc := *NewPodPlacementConfig().
				WithName("nil-cel-ppc").
				WithNamespace("default").
				WithPriority(100).
				Build()
			// Ensure Plugins is non-nil but CelArchitecturePlacement is nil
			ppc.Spec.Plugins = &plugins.LocalPlugins{}

			pod := NewPod().WithName("test-pod").WithNamespace("default").Build()
			wrappedPod := newPod(pod, context.Background(), recorder)

			Expect(func() {
				handled := reconciler.applyCELArchitecturePlacement(context.Background(), ppc, wrappedPod)
				Expect(handled).To(BeFalse(),
					"controller path must return false when CelArchitecturePlacement is nil")
			}).NotTo(Panic(), "controller path must not panic when CelArchitecturePlacement is nil")

			// Verify pod was not modified
			Expect(wrappedPod.Spec.Affinity).To(BeNil(),
				"pod affinity must not be set when CelArchitecturePlacement is nil")
		})

		It("should not panic and leave pod unmodified when CelArchitecturePlacement is nil in the webhook path", func() {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(8)

			pod := NewPod().WithName("test-pod").WithNamespace("default").Build()

			ppcObj := NewPodPlacementConfig().
				WithName("nil-cel-wh-ppc").
				WithNamespace("default").
				WithPriority(100).
				Build()
			// Ensure Plugins is non-nil but CelArchitecturePlacement is nil
			ppcObj.Spec.Plugins = &plugins.LocalPlugins{}

			ppcs := []v1beta1.PodPlacementConfig{*ppcObj}
			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)

			Expect(func() {
				wh.applyCELInWebhook(ctx, wrappedPod, ppcs)
			}).NotTo(Panic(), "webhook path must not panic when CelArchitecturePlacement is nil")

			// Verify pod was not modified
			Expect(wrappedPod.Spec.Affinity).To(BeNil(),
				"pod affinity must not be set when CelArchitecturePlacement is nil")
		})
	})

	Context("Multiple PPC Iteration", func() {
		It("should skip disabled, empty-result, and malformed PPCs and apply the highest-priority matching one", func() {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(8)

			pod := NewPod().WithName("multi-ppc-pod").WithNamespace("default").Build()

			ppcs := []v1beta1.PodPlacementConfig{
				// PPC 1: highest priority but CEL disabled — skipped
				*NewPodPlacementConfig().WithName("ppc-disabled").WithNamespace("default").WithPriority(250).
					WithCelArchitecturePlacement(false, []string{utils.ArchitectureS390x},
						[]plugins.ArchitectureRule{NewRule("always", "true", utils.ArchitectureS390x)}).Build(),
				// PPC 2: high priority, non-matching rule, empty fallback — produces no architectures, skipped
				*NewPodPlacementConfig().WithName("ppc-no-match").WithNamespace("default").WithPriority(200).
					WithCelArchitecturePlacement(true, []string{},
						[]plugins.ArchitectureRule{NewRule("no-match", "self.metadata.name == 'other'", utils.ArchitectureArm64)}).Build(),
				// PPC 3: medium priority with malformed CEL — allRulesErrored, skipped
				*NewPodPlacementConfig().WithName("ppc-malformed").WithNamespace("default").WithPriority(150).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
						[]plugins.ArchitectureRule{NewRule("bad", "self.metadata.name ==", utils.ArchitectureAmd64)}).Build(),
				// PPC 4: lowest priority with matching rule — this should win
				*NewPodPlacementConfig().WithName("ppc-match").WithNamespace("default").WithPriority(100).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
						[]plugins.ArchitectureRule{NewRule("match-all", "true", utils.ArchitecturePpc64le)}).Build(),
			}

			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)

			archs := extractArchitectures(wrappedPod.PodObject())
			Expect(archs).To(ConsistOf(utils.ArchitecturePpc64le),
				"should apply ppc64le from the lowest-priority matching PPC after skipping disabled, non-matching, and malformed PPCs")
		})

		It("should use fallback from the first matching PPC when no rules match", func() {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(8)

			pod := NewPod().WithName("fallback-pod").WithNamespace("default").Build()

			ppcs := []v1beta1.PodPlacementConfig{
				// PPC 1: all rules malformed -- skip entirely
				*NewPodPlacementConfig().WithName("ppc-all-bad").WithNamespace("default").WithPriority(200).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureS390x},
						[]plugins.ArchitectureRule{NewRule("bad1", "self.metadata ==", utils.ArchitectureS390x)}).Build(),
				// PPC 2: valid rules but none match -- use this PPC's fallback
				*NewPodPlacementConfig().WithName("ppc-fallback").WithNamespace("default").WithPriority(100).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureArm64},
						[]plugins.ArchitectureRule{NewRule("no-match", "self.metadata.name == 'nope'", utils.ArchitecturePpc64le)}).Build(),
			}

			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)

			archs := extractArchitectures(wrappedPod.PodObject())
			Expect(archs).To(ConsistOf(utils.ArchitectureArm64),
				"should use arm64 fallback from the second PPC after first PPC is fully malformed")
		})
	})

	Context("Nil vs Empty Slice Edge Cases", func() {
		It("should return fallback when rules is nil but fallbackArchitectures is non-nil", func() {
			fallback := []string{utils.ArchitectureAmd64}
			result, err := evaluateCELArchitecturePlacement(nil, fallback,
				NewPod().WithName("nil-rules-pod").WithNamespace("default").Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(result.matched).To(BeFalse())
			Expect(result.architectures).To(ConsistOf(utils.ArchitectureAmd64))
		})
	})

	Context("Priority Boundary", func() {
		It("should evaluate PPC with priority 0 (minimum boundary)", func() {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(8)

			pod := NewPod().WithName("priority-zero-pod").WithNamespace("default").Build()
			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().WithName("ppc-zero").WithNamespace("default").WithPriority(0).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
						[]plugins.ArchitectureRule{NewRule("match", "true", utils.ArchitecturePpc64le)}).Build(),
			}

			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)

			archs := extractArchitectures(wrappedPod.PodObject())
			Expect(archs).To(ConsistOf(utils.ArchitecturePpc64le),
				"PPC with priority 0 should still be evaluated and applied")
		})
	})

	Context("CEL no-match falls through to image-based detection", func() {
		It("should not modify pod when all PPCs have malformed CEL rules (allowing image-based fallthrough)", func() {
			ctx := context.Background()
			recorder := record.NewFakeRecorder(8)

			pod := NewPod().WithName("fallthrough-pod").WithNamespace("default").Build()

			ppcs := []v1beta1.PodPlacementConfig{
				*NewPodPlacementConfig().WithName("ppc-bad-1").WithNamespace("default").WithPriority(200).
					WithCelArchitecturePlacement(true, []string{utils.ArchitectureAmd64},
						[]plugins.ArchitectureRule{NewRule("bad-1", "self.metadata.name ==", utils.ArchitectureArm64)}).Build(),
				*NewPodPlacementConfig().WithName("ppc-bad-2").WithNamespace("default").WithPriority(100).
					WithCelArchitecturePlacement(true, []string{utils.ArchitecturePpc64le},
						[]plugins.ArchitectureRule{NewRule("bad-2", "self.metadata ==", utils.ArchitectureS390x)}).Build(),
			}

			wh := &PodSchedulingGateMutatingWebHook{}
			wrappedPod := newPod(pod, ctx, recorder)
			wh.applyCELInWebhook(ctx, wrappedPod, ppcs)

			// Pod should be completely unmodified -- no affinity, no nodeSelector
			Expect(pod.Spec.Affinity).To(BeNil(),
				"pod should have no affinity when all PPCs are malformed, allowing image-based detection to run")
			Expect(pod.Spec.NodeSelector).To(BeNil())
		})
	})
})
