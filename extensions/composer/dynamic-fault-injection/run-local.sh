#!/usr/bin/env sh
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

#

CONFIG=$(cat <<-END
{
	"endpoints": [
		{
			"match": {"prefix": "/"},
			"responses": [
				{
				  "status": 200,
				  "resolution": 1000,
				  "distribution": {
				  "p0.0": "30ms",
				  "p100.0": "500ms"
				  }
				},
				{
				  "status": 503,
				  "resolution": 1000,
				  "distribution": {
			        "p0.0": "3ms",
                    "p100.0": "5ms"
                  }
				}
			]
		}
	]
}
END
)
echo $CONFIG
../../../cli/out/boe* run \
    --log-level dynamic_modules:debug \
    --cluster-insecure localhost:8042 \
    --test-upstream-cluster localhost:8042 \
    --local $(pwd) \
    --config "${CONFIG}"
