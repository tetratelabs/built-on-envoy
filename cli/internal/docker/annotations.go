// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package docker

// OCI annotation keys
const (
	AnnotationLicenses    = "org.opencontainers.image.licenses"
	AnnotationTitle       = "org.opencontainers.image.title"
	AnnotationVersion     = "org.opencontainers.image.version"
	AnnotationDescription = "org.opencontainers.image.description"
	AnnotationCreated     = "org.opencontainers.image.created"
	AnnotationAuthors     = "org.opencontainers.image.authors"
	AnnotationSource      = "org.opencontainers.image.source"
	AnnotationRevision    = "org.opencontainers.image.revision"
)

// Built On Envoy specific annotation keys
const (
	AnnotationExtensionType            = "io.tetratelabs.built-on-envoy.extension.type"
	AnnotationExtensionArtifact        = "io.tetratelabs.built-on-envoy.extension.artifact"
	AnnotationExtensionComposerVersion = "io.tetratelabs.built-on-envoy.extension.composer_version"

	AnnotationExtensionArtifactBinary = "binary"
	AnnotationExtensionArtifactSource = "source"
)

// AnnotationPrefix returns the appropriate prefix for OCI annotations based on platform count.
// For multi-platform images, annotations should be prefixed to apply to the index.
// For single-platform images, annotations can be applied directly to the manifest.
func AnnotationPrefix(platformCount int) string {
	if platformCount > 1 {
		return "index,manifest:"
	}
	return ""
}
