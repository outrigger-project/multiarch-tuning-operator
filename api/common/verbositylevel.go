package common

// LogVerbosityLevel is a type derived from string used to represent the log level in operands' CRDs.
// +kubebuilder:validation:Enum=Normal;Debug;Trace;TraceAll
type LogVerbosityLevel string

const (
	LogVerbosityLevelNormal   LogVerbosityLevel = "Normal"
	LogVerbosityLevelDebug    LogVerbosityLevel = "Debug"
	LogVerbosityLevelTrace    LogVerbosityLevel = "Trace"
	LogVerbosityLevelTraceAll LogVerbosityLevel = "TraceAll"
)

func (verbosity LogVerbosityLevel) ToZapLevelInt() int {
	logVerbosityToZapLevelMap := map[LogVerbosityLevel]int{
		LogVerbosityLevelNormal:   0,
		LogVerbosityLevelDebug:    1,
		LogVerbosityLevelTrace:    2,
		LogVerbosityLevelTraceAll: 3,
	}

	if level, ok := logVerbosityToZapLevelMap[verbosity]; ok {
		return level
	}
	return logVerbosityToZapLevelMap[LogVerbosityLevelNormal]
}
