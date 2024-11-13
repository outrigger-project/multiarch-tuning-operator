package podplacement

import (
	"testing"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
)

func TestBasePlugin_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{"Enabled Plugin", true, true},
		{"Disabled Plugin", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &plugins.BasePlugin{Enabled: tt.enabled}
			if plugin.IsEnabled() != tt.expected {
				t.Errorf("Expected IsEnabled() to be %v, got %v", tt.expected, plugin.IsEnabled())
			}
		})
	}
}

func TestBasePlugin_Name(t *testing.T) {
	plugin := &plugins.BasePlugin{}
	if plugin.Name() != "BasePlugin" {
		t.Errorf("Expected Name() to return 'BasePlugin', got %s", plugin.Name())
	}
}
