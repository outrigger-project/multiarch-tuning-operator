package builder

import (
	ocpconfigv1 "github.com/openshift/api/config/v1"
)

// ImageTagMirrorsBuilder is a builder for ocpconfigv1.ImageTagMirrorSet objects to be used only in unit tests.
type ImageTagMirrorsBuilder struct {
	imageTagMirrors *ocpconfigv1.ImageTagMirrors
}

// NewImageTagMirrors returns a new ImageTagMirrorsBuilder to build ocpconfigv1.ImageTagMirrorSet objects. It is meant to be used only in unit tests.
func NewImageTagMirrors() *ImageTagMirrorsBuilder {
	return &ImageTagMirrorsBuilder{
		imageTagMirrors: &ocpconfigv1.ImageTagMirrors{},
	}
}

func (t *ImageTagMirrorsBuilder) WithMirrors(values ...ocpconfigv1.ImageMirror) *ImageTagMirrorsBuilder {
	t.imageTagMirrors.Mirrors = append(t.imageTagMirrors.Mirrors, values...)
	return t
}

func (t *ImageTagMirrorsBuilder) WithSource(registry string) *ImageTagMirrorsBuilder {
	t.imageTagMirrors.Source = registry
	return t
}

func (t *ImageTagMirrorsBuilder) WithMirrorSourcePolicy(policy ocpconfigv1.MirrorSourcePolicy) *ImageTagMirrorsBuilder {
	t.imageTagMirrors.MirrorSourcePolicy = policy
	return t
}

func (t *ImageTagMirrorsBuilder) WithMirrorAllowContactingSource() *ImageTagMirrorsBuilder {
	return t.WithMirrorSourcePolicy(ocpconfigv1.AllowContactingSource)
}

func (t *ImageTagMirrorsBuilder) WithMirrorNeverContactSource() *ImageTagMirrorsBuilder {
	return t.WithMirrorSourcePolicy(ocpconfigv1.NeverContactSource)
}

func (t *ImageTagMirrorsBuilder) Build() *ocpconfigv1.ImageTagMirrors {
	return t.imageTagMirrors
}
