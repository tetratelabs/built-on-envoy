// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Credentials holds authentication credentials for a registry.
type Credentials struct {
	Username string
	Password string
}

// ClientOptions configures a remote repository.
type ClientOptions struct {
	Credentials *Credentials
	PlainHTTP   bool
}

// newRemoteClient creates a new auth.Client for the specified registry with optional credentials.
func newRemoteClient(registry string, opts *ClientOptions) remote.Client {
	client := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}
	if opts != nil && opts.Credentials != nil {
		client.Credential = auth.StaticCredential(registry, auth.Credential{
			Username: opts.Credentials.Username,
			Password: opts.Credentials.Password,
		})
	}
	return client
}
