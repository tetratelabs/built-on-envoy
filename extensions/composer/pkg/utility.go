package pkg

import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

func GetMostSpecificConfig[T any](handle shared.HttpFilterHandle) T {
	var zero T
	mostSpecificConfig := handle.GetMostSpecificConfig()
	if mostSpecificConfig == nil {
		return zero
	}

	config, ok := mostSpecificConfig.(T)
	if !ok {
		handle.Log(shared.LogLevelDebug, "most specific config is not of expected type: %T", mostSpecificConfig)
		return zero
	}

	return config
}
