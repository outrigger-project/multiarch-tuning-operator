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

package plugins

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"

	apiservercel "k8s.io/apiserver/pkg/cel"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var (
	// validationEnv is the package-level typed CEL environment used by
	// ValidateCELExpressions. It is created once and reused across all webhook
	// calls to avoid rebuilding the environment (and recompiling all expressions)
	// on every admission request, which would exceed the Kubernetes webhook
	// timeout when a PPC contains hundreds of rules.
	validationEnv    *cel.Env
	validationEnvErr error
	validationOnce   sync.Once
)

// getOrCreateValidationEnv returns the shared typed CEL validation environment,
// creating it on the first call. Subsequent calls return the cached result.
// If initialisation fails the first time, every subsequent call returns the
// same error (same design as getOrCreateEvaluator in cel_evaluator.go).
func getOrCreateValidationEnv() (*cel.Env, error) {
	validationOnce.Do(func() {
		tp, err := podValidationTypeProvider()
		if err != nil {
			validationEnvErr = fmt.Errorf("failed to create pod type provider: %w", err)
			return
		}

		baseEnv, err := cel.NewEnv()
		if err != nil {
			validationEnvErr = fmt.Errorf("failed to create base CEL environment: %w", err)
			return
		}

		envOpts, err := tp.EnvOptions(baseEnv.CELTypeProvider())
		if err != nil {
			validationEnvErr = fmt.Errorf("failed to build CEL environment options from type provider: %w", err)
			return
		}

		envOpts = append(envOpts, cel.Variable("self", cel.ObjectType("pod")))

		env, err := cel.NewEnv(envOpts...)
		if err != nil {
			validationEnvErr = fmt.Errorf("failed to create CEL environment: %w", err)
			return
		}

		validationEnv = env
	})

	if validationEnvErr != nil {
		return nil, validationEnvErr
	}
	return validationEnv, nil
}

// +kubebuilder:object:generate=true

const (
	// celArchitecturePlacementPluginName stores the name for the celArchitecturePlacement plugin.
	celArchitecturePlacementPluginName = "celArchitecturePlacement"
)

// CelArchitecturePlacement is a plugin that provides CEL-based architecture selection rules.
// This plugin is only available in namespace-scoped PodPlacementConfig resources.
// When a rule matches, the plugin removes any existing architecture constraints from the pod's
// nodeSelector and nodeAffinity, then sets new architecture constraints based on the rule.
type CelArchitecturePlacement struct {
	// BasePlugin controls whether CEL architecture placement is enabled.
	BasePlugin `json:",inline"`

	// FallbackArchitectures is a required list of 1 to 4 architectures to use when no rules match.
	// This limits the explosion of possible rules by providing a sensible default.
	// When applied, existing architecture constraints are removed and replaced with these architectures.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	FallbackArchitectures []string `json:"fallbackArchitectures" protobuf:"bytes,2,rep,name=fallbackArchitectures"`

	// Rules is a list of architecture selection rules evaluated in order.
	// If omitted, the fallback architectures are used for all selected pods.
	// The first matching rule determines the target architectures.
	// When a rule matches, existing architecture constraints are removed and replaced.
	// Maximum of 1000 rules per configuration to prevent excessive evaluation time during
	// pod admission. This limit balances flexibility with performance, ensuring CEL
	// evaluation completes within acceptable latency bounds (microseconds per expression).
	// The limit may be adjusted based on production usage patterns and performance data.
	// +optional
	// +kubebuilder:validation:MaxItems=1000
	Rules []ArchitectureRule `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`
}

// ArchitectureRule defines a single CEL-based rule for architecture selection
type ArchitectureRule struct {
	// Name is a descriptive name for this rule, with a length of 1 to 253 characters.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// Expression is a CEL expression that evaluates against a Pod resource.
	// It must contain 1 to 1024 characters.
	// A true result selects this rule's architectures; a false result advances to the next rule.
	// The expression has access to the pod via the 'self' variable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Expression string `json:"expression" protobuf:"bytes,2,opt,name=expression"`

	// Architectures is a list of 1 to 4 target architectures to use when this rule matches.
	// When applied, any existing architecture constraints in the pod's nodeSelector
	// and nodeAffinity are removed and replaced with these architectures.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	Architectures []string `json:"architectures" protobuf:"bytes,3,rep,name=architectures"`
}

// Name returns the name of the celArchitecturePlacementPluginName.
func (c *CelArchitecturePlacement) Name() string {
	return celArchitecturePlacementPluginName
}

// ValidateArchitectures checks whether the architectures are valid
func (c *CelArchitecturePlacement) ValidateArchitectures() error {
	validArchs := map[string]bool{
		utils.ArchitectureAmd64:   true,
		utils.ArchitectureArm64:   true,
		utils.ArchitecturePpc64le: true,
		utils.ArchitectureS390x:   true,
	}

	// Validate fallback architectures
	for _, arch := range c.FallbackArchitectures {
		if !validArchs[arch] {
			return fmt.Errorf("invalid fallback architecture: %s", arch)
		}
	}

	// Validate rule architectures
	for _, rule := range c.Rules {
		for _, arch := range rule.Architectures {
			if !validArchs[arch] {
				return fmt.Errorf("invalid architecture in rule %s: %s", rule.Name, arch)
			}
		}
	}

	return nil
}

// podValidationTypeProvider returns a DeclTypeProvider that models only the Pod
// metadata fields permitted by the CEL allow-list. Using a typed provider lets
// the CEL checker reject invalid field references (e.g. self.metadata.labells)
// at admission time rather than silently returning null at runtime.
//
// The schema is intentionally minimal:
//
//	self → pod (object)
//	    .metadata → pod.metadata (object)
//	        .name            string
//	        .namespace       string
//	        .generateName    string
//	        .labels          map<string,string>
//	        .annotations     map<string,string>
//
// spec and status are NOT defined, so self.spec.* fails compile-time type
// checking in addition to being rejected by assertMetadataOnly.
func podValidationTypeProvider() (*apiservercel.DeclTypeProvider, error) {
	strToStr := apiservercel.NewMapType(apiservercel.StringType, apiservercel.StringType, 0)

	metadataFields := map[string]*apiservercel.DeclField{
		"name":         apiservercel.NewDeclField("name", apiservercel.StringType, false, nil, nil),
		"namespace":    apiservercel.NewDeclField("namespace", apiservercel.StringType, false, nil, nil),
		"generateName": apiservercel.NewDeclField("generateName", apiservercel.StringType, false, nil, nil),
		"labels":       apiservercel.NewDeclField("labels", strToStr, false, nil, nil),
		"annotations":  apiservercel.NewDeclField("annotations", strToStr, false, nil, nil),
	}
	metadataType := apiservercel.NewObjectType("pod.metadata", metadataFields)

	podFields := map[string]*apiservercel.DeclField{
		"metadata": apiservercel.NewDeclField("metadata", metadataType, false, nil, nil),
	}
	podType := apiservercel.NewObjectType("pod", podFields)

	tp := apiservercel.NewDeclTypeProvider(podType)
	return tp, nil
}

// ValidateCELExpressions validates all CEL expressions in the plugin's rules
// at admission time. Validation ensures:
//  1. Each expression compiles without syntax errors.
//  2. Each expression returns a boolean value.
//  3. Invalid field references (e.g. self.metadata.labells) are rejected via
//     Pod schema type checking using a typed CEL environment.
//  4. No expression references self.spec.* or self.status.* fields
//     (assertMetadataOnly: defense-in-depth, enforced after type checking).
//
// The typed environment binds 'self' to the pod object type (cel.ObjectType("pod"))
// so that the CEL checker can statically verify field names against the known
// Pod metadata schema. This satisfies enhancement MTO-0005 §"CEL Expression Validation"
// item 3 ("Pod Schema Validation").
//
// The runtime evaluator (newCELEvaluator) intentionally uses cel.DynType and
// evaluates against podToMap() data — it never validates schema and never
// exposes spec/status.
func (c *CelArchitecturePlacement) ValidateCELExpressions() error {
	// Reuse the package-level typed CEL environment. Building the environment
	// from scratch on every webhook call is too expensive when a PPC has
	// hundreds of rules: on a CI runner 500+ compilations easily exceed the
	// Kubernetes webhook admission timeout of 10 s.
	env, err := getOrCreateValidationEnv()
	if err != nil {
		return err
	}

	for _, rule := range c.Rules {
		ast, issues := env.Compile(rule.Expression)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("invalid CEL expression in rule %q: CEL compilation error: %w", rule.Name, issues.Err())
		}
		if ast.OutputType() != cel.BoolType {
			return fmt.Errorf("invalid CEL expression in rule %q: CEL expression must return a boolean, got %v", rule.Name, ast.OutputType())
		}
		// Defense-in-depth: assertMetadataOnly walks the AST to reject any
		// self.spec.* or self.status.* path that the typed environment
		// (which only defines self.metadata.*) might have somehow admitted.
		if err := assertMetadataOnly(ast, rule.Name); err != nil {
			return err
		}
	}

	return nil
}

// assertMetadataOnly inspects the compiled CEL AST and returns an error if any
// field-selection path starting from the 'self' variable descends into
// self.spec or self.status. Only self.metadata.* is permitted.
//
// Enhancement reference: §"CEL Data Scope and Field Allow-list"
// "Any reference to self.spec or self.status … is rejected at PodPlacementConfig
// create/update time (assertMetadataOnly)."
func assertMetadataOnly(ast *cel.Ast, ruleName string) error {
	nav := celast.NavigateAST(ast.NativeRep())
	selectNodes := celast.MatchDescendants(nav, celast.KindMatcher(celast.SelectKind))
	for _, node := range selectNodes {
		path := selectPath(node)
		// path is in order from root to leaf, e.g.
		// self.spec.containers → ["self", "spec", "containers"]
		// Reject any path of the form ["self", "spec", ...] or ["self", "status", ...]
		if len(path) >= 2 && path[0] == "self" {
			if path[1] == "spec" || path[1] == "status" {
				return fmt.Errorf("invalid CEL expression in rule %q: "+
					"references a disallowed field 'self.%s': "+
					"only self.metadata.* is permitted",
					ruleName, strings.Join(path[1:], "."))
			}
		}
	}
	return nil
}

// selectPath reconstructs the dotted field path for a select expression node
// by walking the operand chain back to the root identifier.
// Returns the path segments from root to leaf, e.g. ["self", "spec", "containers"].
// Returns nil if the root is not an identifier (e.g. a function call result).
func selectPath(node celast.NavigableExpr) []string {
	var segments []string
	cur := celast.Expr(node)
	for cur.Kind() == celast.SelectKind {
		sel := cur.AsSelect()
		segments = append([]string{sel.FieldName()}, segments...)
		cur = sel.Operand()
	}
	if cur.Kind() == celast.IdentKind {
		segments = append([]string{cur.AsIdent()}, segments...)
		return segments
	}
	return nil
}
