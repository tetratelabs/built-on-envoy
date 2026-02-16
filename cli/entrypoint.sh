#!/bin/sh
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# /var/boe is a named volume that is automatically created when running the CLI in Docker mode.
# To make sure the 'boe' user has permissions to write to this directory, we change the ownership
# of the directory to 'boe'.
chown -R boe:boe /var/boe
exec gosu boe "$@"
