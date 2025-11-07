package plugins

import (
	"testing"
)

func TestBasePlugin_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"Enabled Plugin", true},
		{"Disabled Plugin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &BasePlugin{Enabled: tt.enabled}
			if plugin.IsEnabled() != tt.enabled {
				t.Errorf("Expected IsEnabled() to be %v, got %v", tt.enabled, plugin.IsEnabled())
			}
		})
	}
}

func TestBasePlugin_Name(t *testing.T) {
	plugin := &BasePlugin{}
	if plugin.Name() != "BasePlugin" {
		t.Errorf("Expected Name() to return 'BasePlugin', got %s", plugin.Name())
	}
}

func TestNodeAffinityScoring_Name(t *testing.T) {
	plugin := &NodeAffinityScoring{}

	if plugin.Name() != NodeAffinityScoringPluginName {
		t.Errorf("Expected plugin name %s, but got %s", NodeAffinityScoringPluginName, plugin.Name())
	}
}

func TestExecFormatErrorMonitor_Name(t *testing.T) {
	plugin := &ExecFormatErrorMonitor{}

	if plugin.Name() != ExecFormatErrorMonitorPluginName {
		t.Errorf("Expected plugin name %s, but got %s", ExecFormatErrorMonitorPluginName, plugin.Name())
	}
}
