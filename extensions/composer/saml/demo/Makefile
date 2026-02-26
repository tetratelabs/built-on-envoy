# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

MAKEDIR := $(shell pwd)

GO_ARCH := $(shell go env GOARCH)
GO_OS := $(shell go env GOOS)

BOE_BIN := $(MAKEDIR)/../../../../cli/out/boe-$(GO_OS)-$(GO_ARCH)

KEYCLOACK_ADDRESS ?= http://localhost:8080

TEMP_DIR := $(MAKEDIR)/gen
IDP_METADATA_FILE := $(TEMP_DIR)/idp-metadata.xml
SP_CERT_FILE := $(TEMP_DIR)/sp-cert.pem
SP_KEY_FILE := $(TEMP_DIR)/sp-key.pem

.PHONY: keycloak-up
keycloak-up: ## Every time keycloak is started the SAML idP metadata is regenerated and needs to be downloaded. Make run takes care of this.
	echo "Starting Keycloak at $(KEYCLOACK_ADDRESS)..."
	cd $(MAKEDIR)/keycloak && docker compose up

.PHONY: keycloak-down
keycloak-down: ## Stop the Keycloak container
	cd $(MAKEDIR)/keycloak && docker compose down -v

.PHONY: run ## Run the plugin with the SAML IdP metadata and SP certificates
run: ${BOE_BIN} ${IDP_METADATA_FILE}
	#${BOE_BIN} run --local .. --log-level dynamic_modules:trace --config '{"entity_id": "saml-sp","acs_path": "/acs", "idp_metadata_xml": {"file": "$(IDP_METADATA_FILE)"}}'
	${BOE_BIN} run --local .. --log-level dynamic_modules:trace --config '{"entity_id": "http://localhost:10000","acs_path": "/acs", "idp_metadata_xml": {"file": "$(IDP_METADATA_FILE)"}}'

.PHONY: clean
clean: keycloak-down
	rm -rf $(TEMP_DIR)

## Generate required files

${TEMP_DIR}:
	@mkdir -p $(TEMP_DIR)

${IDP_METADATA_FILE}: ${TEMP_DIR}
	curl $(KEYCLOACK_ADDRESS)/realms/saml-demo/protocol/saml/descriptor -o ${IDP_METADATA_FILE}

${BOE_BIN}:
	${MAKE} -C ../../../../cli build
