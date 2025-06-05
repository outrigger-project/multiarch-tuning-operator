package image

import (
	"reflect"
	"testing"
)

func Test_matchAndExpandGlob(t *testing.T) {
	type args struct {
		registry       string
		imageReference string
	}
	tests := []struct {
		name        string
		args        args
		expectedOk  bool
		expectedURL string
	}{
		{
			name: "*.kubernetes.io will not match kubernetes.io",
			args: args{
				registry:       "*.kubernetes.io",
				imageReference: "kubernetes.io/foo:bar",
			},
			expectedOk: false,
		},
		{
			name: "*.kubernetes.io will match abc.kubernetes.io",
			args: args{
				registry:       "*.kubernetes.io",
				imageReference: "abc.kubernetes.io/foo:bar",
			},
			expectedOk:  true,
			expectedURL: "abc.kubernetes.io",
		},
		{
			name: "*.*.kubernetes.io will not match abc.kubernetes.io",
			args: args{
				registry:       "*.*.kubernetes.io",
				imageReference: "abc.kubernetes.io/foo:bar",
			},
			expectedOk: false,
		},
		{
			name: "*.*.kubernetes.io will match abc.def.kubernetes.io",
			args: args{
				registry:       "*.*.kubernetes.io",
				imageReference: "abc.def.kubernetes.io/baz/foo:bar",
			},
			expectedOk:  true,
			expectedURL: "abc.def.kubernetes.io",
		},
		{
			name: "prefix.*.io will match prefix.kubernetes.io",
			args: args{
				registry:       "prefix.*.io",
				imageReference: "prefix.kubernetes.io/foo:bar",
			},
			expectedOk:  true,
			expectedURL: "prefix.kubernetes.io",
		},
		{
			name: "*-good.kubernetes.io will match prefix-good.kubernetes.io",
			args: args{
				registry:       "*-good.kubernetes.io",
				imageReference: "prefix-good.kubernetes.io/baz/foo:bar",
			},
			expectedOk:  true,
			expectedURL: "prefix-good.kubernetes.io",
		},
		{
			name: "no glob. Registry should not be processed for expansion",
			args: args{
				registry:       "kubernetes.io",
				imageReference: "kubernetes.io/foo:bar",
			},
			expectedOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotOk := matchAndExpandGlob(tt.args.registry, tt.args.imageReference)
			if gotOk != tt.expectedOk {
				t.Errorf("globMatch() = %v, expectedOk %v", gotOk, tt.expectedOk)
			}
			if gotOk && gotURL != tt.expectedURL {
				t.Errorf("globMatch() = %v, expectedURL %v", gotURL, tt.expectedURL)
			}
		})
	}
}

func Test_authCfg_expandGlobs(t *testing.T) {
	type fields struct {
		Auths map[string]authData
	}
	type args struct {
		imageReference string
	}
	const cred = "dXNlcm5hbWU6cGFzc3dvcmQ="
	tests := []struct {
		name                string
		fields              fields
		args                args
		additionalAuthsWant *authCfg
	}{
		{
			name: "no globs",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "docker.io/foo:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{},
			},
		},
		{
			name: "with globs and no match",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "docker.io/foo:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{},
			},
		},
		{
			name: "with globs, match but existing auth for the same registry. Should ignore adding the resolved glob",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io/my-repo": {
						Auth: cred,
					},
					"abc.kubernetes.io/my-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.kubernetes.io/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{},
			},
		},
		{
			name: "with globs, match and no existing auth for the same registry/repo",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io": {
						Auth: cred,
					},
					"abc.kubernetes.io/my-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.kubernetes.io/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{
					"abc.kubernetes.io": {
						Auth: cred,
					},
				},
			},
		},
		{
			name: "with globs, match and no existing auth for the same registry",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io": {
						Auth: cred,
					},
					"abc.kubernetes.io/other-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.kubernetes.io/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{
					"abc.kubernetes.io": {
						Auth: cred,
					},
				},
			},
		},
		{
			name: "with globs, match and no existing auth for the same registry/repo",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io/my-repo": {
						Auth: cred,
					},
					"abc.kubernetes.io/other-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.kubernetes.io/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{
					"abc.kubernetes.io/my-repo": {
						Auth: cred,
					},
				},
			},
		},
		{
			name: "with globs, match and glob including port number",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io:443": {
						Auth: cred,
					},
					"abc.kubernetes.io:443/other-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.kubernetes.io:443/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{
					"abc.kubernetes.io:443": {
						Auth: cred,
					},
				},
			},
		},
		{
			name: "with two globs, match and glob including port number",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io:443": {
						Auth: cred,
					},
					"abc.kubernetes.io:443/other-repo": {
						Auth: cred,
					},
					"*.*.kubernetes.io:443": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.def.kubernetes.io:443/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{
					"abc.def.kubernetes.io:443": {
						Auth: cred,
					},
				},
			},
		},
		{
			name: "with one glob for 3rd level domain, should not match 4th level domain",
			fields: fields{
				Auths: map[string]authData{
					"docker.io": {
						Auth: cred,
					},
					"*.kubernetes.io:443": {
						Auth: cred,
					},
					"abc.kubernetes.io:443/other-repo": {
						Auth: cred,
					},
				},
			},
			args: args{
				imageReference: "abc.def.kubernetes.io:443/my-repo/my-image:bar",
			},
			additionalAuthsWant: &authCfg{
				Auths: map[string]authData{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := authCfg{
				Auths: tt.fields.Auths,
			}
			// merge initial auths with the expanded globs
			want := ac.Auths
			for k, v := range tt.additionalAuthsWant.Auths {
				want[k] = v
			}
			if got := ac.expandGlobs(tt.args.imageReference); !reflect.DeepEqual(got, &authCfg{
				Auths: want,
			}) {
				t.Errorf("expandGlobs() = %v, want %v", got, want)
			}
		})
	}
}
