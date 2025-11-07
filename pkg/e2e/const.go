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
	MyFakeITMSAllowContactSourceTestMirrorRegistry      = "my-fake-itms-allow-contact-source-mirror-registry.io"
	MyFakeITMSNeverContactSourceTestMirrorRegistry      = "my-fake-itms-never-contact-source-mirror-registry.io"
	MyFakeIDMSNeverContactSourceTestSourceRegistry      = "my-fake-idms-never-contact-source-source-registry.io"
	MyFakeICSPAllowContactSourceTestSourceRegistry      = "my-fake-icsp-allow-contact-source-source-registry.io"
	HelloopenshiftPublicMultiarchImage                  = "quay.io/openshifttest/hello-openshift:1.2.0"
	HelloopenshiftPublicMultiarchImageDigest            = "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83"
	HelloopenshiftPublicMultiarchImageTagDigest         = "quay.io/openshifttest/hello-openshift:1.2.0@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83"
	HelloopenshiftPublicMultiarchImageWithPortDigest    = "quay.io:443/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83"
	HelloopenshiftPublicMultiarchImageWithPortTag       = "quay.io:443/openshifttest/hello-openshift:1.2.0"
	HelloopenshiftPublicMultiarchImageWithPortTagDigest = "quay.io:443/openshifttest/hello-openshift:1.2.0@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83"
	SleepPublicMultiarchImage                           = "quay.io/openshifttest/sleep:1.2.0"
	RedisPublicMultiarchImage                           = "gcr.io/google_containers/redis:v1"
	PausePublicMultiarchImage                           = "gcr.io/google_containers/pause:3.2"
	ITMSName                                            = "mto-itms-test"
	IDMSName                                            = "mto-idms-test"
	ICSPName                                            = "mto-icsp-test"
	RegistryNamespace                                   = "registry"
	InsecureRegistryName                                = "insecure"
	NotTrustedRegistryName                              = "not-trusted"
	TrustedRegistryName                                 = "trusted"
)
