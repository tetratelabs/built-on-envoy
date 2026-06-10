// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"strconv"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	// DefaultOCIRegistry is the default OCI registry to push extensions to.
	DefaultOCIRegistry = "ghcr.io/tetratelabs/built-on-envoy"
	// DefaultOCISource is the default source URL for extensions.
	// Used when pushing to default registry so that artifacts are linked to the repo.
	// See: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#pushing-container-images
	DefaultOCISource = "https://github.com/tetratelabs/built-on-envoy"

	// OCIAnnotationExtensionType is the OCI annotation key for the extension type.
	OCIAnnotationExtensionType = "io.tetratelabs.built-on-envoy.extension.type"
	// OCIAnnotationFilterType is the OCI annotation key for the extension filter type.
	OCIAnnotationFilterType = "io.tetratelabs.built-on-envoy.extension.filter_type"
	// OCIAnnotationCShared is the OCO annotation key for the cshared flag for Go extensions.
	OCIAnnotationCShared = "io.tetratelabs.built-on-envoy.extension.cshared"
	// OCIAnnotationComposerVersion is the OCI annotation key for the composer version that
	// the extension depends on, if any.
	OCIAnnotationComposerVersion = "io.tetratelabs.built-on-envoy.extension.composer_version"
	// OCIAnnotationArtifact is the OCI annotation key for the extension artifact.
	OCIAnnotationArtifact = "io.tetratelabs.built-on-envoy.extension.artifact"

	// ArtifactBinary indicates the extension artifact is a binary.
	ArtifactBinary = "binary"
	// ArtifactSource indicates the extension artifact is source code.
	ArtifactSource = "source"
)

// RepositoryName constructs the repository name for an extension based
// on the registry and name.
func RepositoryName(registry string, name string) string {
	return registry + "/extension-" + name
}

// NameFromRepository extracts the extension name from the repository URL.
func NameFromRepository(repository string) string {
	// repository is like ghcr.io/tetratelabs/built-on-envoy/extension-cors
	parts := strings.Split(repository, "/")
	if len(parts) == 0 {
		return repository
	}
	lastPart := parts[len(parts)-1]
	return strings.TrimPrefix(lastPart, "extension-")
}

// SourceRepositoryName constructs the source repository name for an extension based on the manifest.
func SourceRepositoryName(registry string, manifest *Manifest) string {
	if (manifest.Type == TypeGo && !manifest.CShared) || manifest.Type == TypeComposer {
		return registry + "/composer-src"
	}
	return registry + "/extension-src-" + manifest.Name
}

// OCIAnnotationsForManifest generates standard OCI image annotations
// from the given extension manifest.
func OCIAnnotationsForManifest(manifest *Manifest) map[string]string {
	filterTypes := make([]string, len(manifest.FilterTypes))
	for i, ft := range manifest.FilterTypes {
		filterTypes[i] = string(ft)
	}
	return map[string]string{
		ocispec.AnnotationTitle:       manifest.Name,
		ocispec.AnnotationDescription: manifest.Description,
		ocispec.AnnotationVersion:     manifest.Version,
		OCIAnnotationComposerVersion:  manifest.ComposerVersion,
		ocispec.AnnotationAuthors:     manifest.Author,
		ocispec.AnnotationLicenses:    manifest.License,
		OCIAnnotationExtensionType:    string(manifest.Type),
		OCIAnnotationFilterType:       strings.Join(filterTypes, ","),
		OCIAnnotationCShared:          strconv.FormatBool(manifest.CShared),
	}
}

// ManifestFromOCI converts an OCI manifest to an extension manifest.
func ManifestFromOCI(manifest *ocispec.Manifest) *Manifest {
	var filterTypes []FilterType
	if raw := manifest.Annotations[OCIAnnotationFilterType]; raw != "" {
		parts := strings.Split(raw, ",")
		filterTypes = make([]FilterType, len(parts))
		for i, p := range parts {
			filterTypes[i] = FilterType(strings.TrimSpace(p))
		}
	}
	fromOCI := &Manifest{
		Name:            manifest.Annotations[ocispec.AnnotationTitle],
		Description:     manifest.Annotations[ocispec.AnnotationDescription],
		Version:         manifest.Annotations[ocispec.AnnotationVersion],
		ComposerVersion: manifest.Annotations[OCIAnnotationComposerVersion],
		Author:          manifest.Annotations[ocispec.AnnotationAuthors],
		License:         manifest.Annotations[ocispec.AnnotationLicenses],
		Type:            Type(manifest.Annotations[OCIAnnotationExtensionType]),
		FilterTypes:     filterTypes,
		CShared:         manifest.Annotations[OCIAnnotationCShared] == "true",
	}
	fromOCI.ApplyDefaults()
	return fromOCI
}
