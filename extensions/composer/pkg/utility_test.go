// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func Test_GetMostSpecificConfig(t *testing.T) {
	type config struct {
		value string
	}

	tests := []struct {
		name           string
		mockConfig     any
		expectedConfig config
	}{
		{
			name:           "returns zero value when no config",
			mockConfig:     nil,
			expectedConfig: config{},
		},
		{
			name:           "returns zero value when config is wrong type",
			mockConfig:     "not a config",
			expectedConfig: config{},
		},
		{
			name:           "returns config when correct type",
			mockConfig:     config{value: "test"},
			expectedConfig: config{value: "test"},
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle := mocks.NewMockHttpFilterHandle(ctrl)
			handle.EXPECT().GetMostSpecificConfig().Return(tt.mockConfig).AnyTimes()
			handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			cfg := GetMostSpecificConfig[config](handle)
			assert.Equal(t, tt.expectedConfig, cfg)
		})
	}
}
