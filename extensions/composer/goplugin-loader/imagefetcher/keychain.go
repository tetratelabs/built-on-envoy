// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package imagefetcher

import (
	"bytes"
	"fmt"

	"github.com/docker/cli/cli/config/configfile"
	dtypes "github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

type pluginKeyChain struct {
	data []byte
}

// Resolve resolves an image reference to a credential from Docker config JSON.
func (k *pluginKeyChain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if bytes.Equal(k.data, []byte("null")) {
		return nil, fmt.Errorf("invalid null credential data")
	}

	cf := configfile.ConfigFile{}
	if err := cf.LoadFromReader(bytes.NewReader(k.data)); err != nil {
		return nil, err
	}

	key := target.RegistryStr()
	if key == name.DefaultRegistry {
		key = authn.DefaultAuthKey
	}

	cfg, err := cf.GetAuthConfig(key)
	if err != nil {
		return nil, err
	}

	empty := dtypes.AuthConfig{}
	if cfg == empty {
		return authn.Anonymous, nil
	}

	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}
