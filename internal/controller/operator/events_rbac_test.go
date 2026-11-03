package operator

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestOperandEventRBAC(t *testing.T) {
	for _, tc := range []struct {
		name       string
		build      func() *rbacv1.ClusterRole
		coreEvents bool
		other      map[string][]string
	}{
		{"controller", buildClusterRoleController, true, map[string][]string{
			"security.openshift.io/securitycontextconstraints": {"use"},
			"/pods": {"list", "watch", "get", "update"}, "/pods/status": {"update"},
			"/configmaps": {"list", "watch", "get"}, "/secrets": {"list", "watch", "get"},
			"multiarch.openshift.io/clusterpodplacementconfigs": {"list", "watch", "get"},
			"multiarch.openshift.io/podplacementconfigs":        {"list", "watch", "get"},
			"authentication.k8s.io/tokenreviews":                {"create"}, "authorization.k8s.io/subjectaccessreviews": {"create"},
		}},
		{"webhook", buildClusterRoleWebhook, false, map[string][]string{
			"/pods": {"list", "watch", "get"},
			"multiarch.openshift.io/clusterpodplacementconfigs": {"list", "watch", "get"},
			"multiarch.openshift.io/podplacementconfigs":        {"list", "watch", "get"},
			"authentication.k8s.io/tokenreviews":                {"create"}, "authorization.k8s.io/subjectaccessreviews": {"create"},
		}},
		{"enoexec", buildClusterRoleENoExecEventsController, true, map[string][]string{
			"/pods,nodes": {"list", "get"}, "/pods,pods/status": {"update"},
			"authentication.k8s.io/tokenreviews": {"create"}, "authorization.k8s.io/subjectaccessreviews": {"create"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var native, core bool
			other := map[string][]string{}
			for _, rule := range tc.build().Rules {
				if slices.Equal(rule.Resources, []string{"events"}) {
					switch {
					case slices.Equal(rule.APIGroups, []string{"events.k8s.io"}):
						native = true
						if len(rule.Verbs) != 3 || !slices.Contains(rule.Verbs, "create") || !slices.Contains(rule.Verbs, "patch") || !slices.Contains(rule.Verbs, "update") {
							t.Errorf("native event permissions: %v", rule.Verbs)
						}
					case slices.Equal(rule.APIGroups, []string{""}):
						core = true
						if !slices.Equal(rule.Verbs, []string{"create", "patch"}) {
							t.Errorf("core event permissions changed: %v", rule)
						}
					default:
						t.Errorf("unexpected events rule: %v", rule)
					}
				} else {
					other[strings.Join(rule.APIGroups, ",")+"/"+strings.Join(rule.Resources, ",")] = rule.Verbs
				}
			}
			if !native {
				t.Error("missing events.k8s.io event writer")
			}
			if core != tc.coreEvents {
				t.Errorf("core event permission=%v want=%v", core, tc.coreEvents)
			}
			if !reflect.DeepEqual(other, tc.other) {
				t.Errorf("unrelated permissions changed: got=%v want=%v", other, tc.other)
			}
		})
	}
}
