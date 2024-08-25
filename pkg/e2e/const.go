package e2e

import "time"

const (
	WaitShort       = 1 * time.Minute
	WaitMedium      = 3 * time.Minute
	WaitOverMedium  = 5 * time.Minute
	WaitLong        = 15 * time.Minute
	WaitOverLong    = 30 * time.Minute
	PollingInterval = 1 * time.Second
	Present         = true
	Absent          = false
)

const (
	MyFakeITMSAllowContactSourceTestMirrorRegistry = "my-fake-itms-allow-contact-source-mirror-registry.io"
	MyFakeITMSAllowContactSourceTestSourceRegistry = "my-fake-itms-allow-contact-source-source-registry.io"
	MyFakeITMSNeverContactSourceTestMirrorRegistry = "my-fake-itms-never-contact-source-mirror-registry.io"
	MyFakeITMSNeverContactSourceTestSourceRegistry = "my-fake-itms-never-contact-source-source-registry.io"
	OpenshifttestPublicMultiarchImage              = "quay.io/openshifttest/hello-openshift:1.2.0"
	SleepPublicMultiarchImage                      = "quay.io/openshifttest/sleep:1.2.0"
	RedisPublicMultiarchImage                      = "gcr.io/google_containers/redis:v1"
	PausePublicMultiarchImage                      = "gcr.io/google_containers/pause:3.2"
	ITMSName                                       = "mto-itms-test"
)
