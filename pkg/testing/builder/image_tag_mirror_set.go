package builder

import (
	ocpconfigv1 "github.com/openshift/api/config/v1"
)

// ImageTagMirrorSetBuilder is a builder for ocpconfigv1.ImageTagMirrorSet objects to be used only in unit tests.
type ImageTagMirrorSetBuilder struct {
	imageTagMirrorSet *ocpconfigv1.ImageTagMirrorSet
}

// NewImageContentSourcePolicy returns a new ImageTagMirrorSetBuilder to build ocpconfigv1.ImageTagMirrorSet objects. It is meant to be used only in unit tests.
func NewImageTagMirrorSet() *ImageTagMirrorSetBuilder {
	return &ImageTagMirrorSetBuilder{
		imageTagMirrorSet: &ocpconfigv1.ImageTagMirrorSet{},
	}
}

func (t *ImageTagMirrorSetBuilder) WithImageTagMirrors(values ...*ocpconfigv1.ImageTagMirrors) *ImageTagMirrorSetBuilder {
	for _, v := range values {
		if v == nil {
			panic("nil value passed to WithImageTagMirrors")
		}
		t.imageTagMirrorSet.Spec.ImageTagMirrors = append(t.imageTagMirrorSet.Spec.ImageTagMirrors, *v)
	}
	return t
}

func (t *ImageTagMirrorSetBuilder) WithName(name string) *ImageTagMirrorSetBuilder {
	t.imageTagMirrorSet.Name = name
	return t
}

func (t *ImageTagMirrorSetBuilder) Build() *ocpconfigv1.ImageTagMirrorSet {
	return t.imageTagMirrorSet
}
