// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internal

import "reflect"

// RedactSensitive returns a copy of any struct with fields tagged `sensitive:"true"` replaced with "*****"
func RedactSensitive(input any) any {
	v := reflect.ValueOf(input)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	clone := reflect.New(v.Type()).Elem()
	clone.Set(v)

	redactStruct(clone)

	return clone.Interface()
}

func redactStruct(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		val := v.Field(i)

		// Handle nested structs recursively
		if val.Kind() == reflect.Struct {
			redactStruct(val)
			continue
		}

		if field.Tag.Get("sensitive") == "true" && val.CanSet() {
			val.Set(reflect.ValueOf("*****"))
		}
	}
}
