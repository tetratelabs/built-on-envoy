// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestRegistryName(t *testing.T) {
	require.Equal(t,
		"ghcr.io/tetratelabs/built-on-envoy/extension-sample",
		RepositoryName(DefaultOCIRegistry, "sample"))
}

func TestNameFromRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		want       string
	}{
		{
			name:       "full repository URL with extension prefix",
			repository: "ghcr.io/tetratelabs/built-on-envoy/extension-cors",
			want:       "cors",
		},
		{
			name:       "repository without extension prefix",
			repository: "ghcr.io/tetratelabs/built-on-envoy/cors",
			want:       "cors",
		},
		{
			name:       "simple name with extension prefix",
			repository: "extension-sample",
			want:       "sample",
		},
		{
			name:       "simple name without extension prefix",
			repository: "sample",
			want:       "sample",
		},
		{
			name:       "empty string",
			repository: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NameFromRepository(tt.repository))
		})
	}
}

func TestSourceRepositoryName(t *testing.T) {
	manifest := &Manifest{Name: "test-extension", Type: TypeLua}
	require.Equal(t,
		"ghcr.io/tetratelabs/built-on-envoy/extension-src-test-extension",
		SourceRepositoryName(DefaultOCIRegistry, manifest))

	composerManifest := &Manifest{Name: "my-set", Type: TypeComposer}
	require.Equal(t,
		"ghcr.io/tetratelabs/built-on-envoy/composer-src",
		SourceRepositoryName(DefaultOCIRegistry, composerManifest))
}

func TestOCIAnnotationsForManifest(t *testing.T) {
	manifest := &Manifest{
		Name:        "test-extension",
		Description: "A test extension",
		Version:     "1.0.0",
		Author:      "Test Author",
		License:     "Apache-2.0",
		Type:        TypeLua,
	}

	annotations := OCIAnnotationsForManifest(manifest)

	require.Equal(t, "test-extension", annotations[ocispec.AnnotationTitle])
	require.Equal(t, "A test extension", annotations[ocispec.AnnotationDescription])
	require.Equal(t, "1.0.0", annotations[ocispec.AnnotationVersion])
	require.Equal(t, "Test Author", annotations[ocispec.AnnotationAuthors])
	require.Equal(t, "Apache-2.0", annotations[ocispec.AnnotationLicenses])
	require.Equal(t, "lua", annotations[OCIAnnotationExtensionType])
}

func TestManifestFromOCI(t *testing.T) {
	ociManifest := &ocispec.Manifest{
		Annotations: map[string]string{
			ocispec.AnnotationTitle:       "test-extension",
			ocispec.AnnotationDescription: "A test extension",
			ocispec.AnnotationVersion:     "1.0.0",
			ocispec.AnnotationAuthors:     "Test Author",
			ocispec.AnnotationLicenses:    "Apache-2.0",
			OCIAnnotationExtensionType:    "lua",
		},
	}

	manifest := ManifestFromOCI(ociManifest)

	require.Equal(t, "test-extension", manifest.Name)
	require.Equal(t, "A test extension", manifest.Description)
	require.Equal(t, "1.0.0", manifest.Version)
	require.Equal(t, "Test Author", manifest.Author)
	require.Equal(t, "Apache-2.0", manifest.License)
	require.Equal(t, TypeLua, manifest.Type)
}
