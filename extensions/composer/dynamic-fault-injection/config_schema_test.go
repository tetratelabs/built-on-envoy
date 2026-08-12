// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `
			{
  "endpoints": [
				    {
				      "match": {
				        "prefix": "/api/"
				      },
				      "responses": [
				        {
				          "status": 200,
				          "resolution": 90,
				          "distribution": {
				            "p0.0": "1ms",
				            "p50.0": "10ms",
				            "p99.0": "200ms"
				          }
				        },
				        {
				          "status": 503,
				          "resolution": 10,
				          "distribution": {
				            "p0.0": "50ms",
				            "p50.0": "100ms",
				            "p99.0": "500ms"
				          }
				        }
				      ]
					}]
			}`)
	})

	// Tried adding the following to the schema to ensure that *either* the `responses` or the `load_based`
	// field would be accepted and documents with both would be rejected. Sadly, it didn't appear to have
	// any effect.
	//
	//    "if": {
	//      "anyOf": [
	//        {"required": ["responses"]}
	//      ]
	//    },
	//    "then": {
	//      "not": {
	//        "required": ["load_based"]
	//      }
	//    },
	//    "if": {
	//      "anyOf": [
	//        {"required": ["load_based"]}
	//      ]
	//    },
	//    "then": {
	//      "not": {
	//        "required": ["responses"]
	//      }
	//    },

	// t.Run("responses and load_based should be mutually exclusive", func(t *testing.T) {
	// 	internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
	// 	  "endpoints": [
	// 	    {
	// 	      "match": {
	// 	        "prefix": "/api/"
	// 	      },
	// 	      "responses": [
	// 	        {
	// 	          "status": 200,
	// 	          "resolution": 90,
	// 	          "distribution": {
	// 	            "p0.0": "1ms",
	// 	            "p50.0": "10ms",
	// 	            "p99.0": "200ms"
	// 	          }
	// 	        },
	// 	        {
	// 	          "status": 503,
	// 	          "resolution": 10,
	// 	          "distribution": {
	// 	            "p0.0": "50ms",
	// 	            "p50.0": "100ms",
	// 	            "p99.0": "500ms"
	// 	          }
	// 	        }
	// 	      ],
	// 	      "load_based": {
	// 	        "healthy": {
	// 	          "threshold_rps": 100,
	// 	          "responses": [
	// 	            {
	// 	              "status": 200,
	// 	              "resolution": 100,
	// 	              "distribution": {
	// 	                "p0.0": "1ms",
	// 	                "p50.0": "5ms",
	// 	                "p99.0": "50ms"
	// 	              }
	// 	            }
	// 	          ]
	// 	        },
	// 	        "tipping_point": {
	// 	          "threshold_rps": 500,
	// 	          "responses": [
	// 	            {
	// 	              "status": 200,
	// 	              "resolution": 50,
	// 	              "distribution": {
	// 	                "p0.0": "50ms",
	// 	                "p50.0": "200ms",
	// 	                "p99.0": "2s"
	// 	              }
	// 	            },
	// 	            {
	// 	              "status": 503,
	// 	              "resolution": 50,
	// 	              "distribution": {
	// 	                "p0.0": "10ms",
	// 	                "p50.0": "50ms",
	// 	                "p99.0": "100ms"
	// 	              }
	// 	            }
	// 	          ],
	// 	          "grey_zone": {
	// 	            "penalty_base": "10ms",
	// 	            "spike_threshold": 0.8,
	// 	            "spike_penalty_duration": "5s",
	// 	            "spike_penalty_multiplier": 2,
	// 	            "recovery_rate": 0.1
	// 	          }
	// 	        }
	// 	      }
	// 	    }
	// 	  ]
	// 	}`)
	// })
}
