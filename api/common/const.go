package common

const SingletonResourceObjectName = "cluster"

type Plugin int

const (
	// MainPlugin checks the core pod placement resources.
	NodeAffinityScoringPluginName Plugin = iota
	// ENoExecPlugin checks the ENoExecEvent resources.
	ExecFormatErrorMonitorPluginName
)
