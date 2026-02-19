# SAML Extension Demo

End-to-end demo of the SAML SP filter using a local Keycloak instance as the
Identity Provider and `boe` to run Envoy with the extension.

## Prerequisites

- Docker and Docker Compose
- `boe` CLI (built automatically by `make run` if not present)

## Quick Start

### 1. Start Keycloak

In one terminal:

```bash
make keycloak-up
```

Wait until Keycloak is ready (~30 seconds). It will be available at
http://localhost:8080 with a pre-configured `saml-demo` realm.

### 2. Run Envoy with the SAML extension

In a second terminal:

```bash
make run
```

This will:
- Download the IdP metadata from Keycloak
- Generate a self-signed SP certificate and key (first run only)
- Build the `boe` CLI if needed
- Start Envoy on **http://localhost:10000** with the SAML filter

### 3. Test the SSO flow

1. Open http://localhost:10000 in your browser.
2. You will be redirected to Keycloak's login page.
3. Log in with the test user:

   | Field    | Value      |
   |----------|------------|
   | Username | `testuser` |
   | Password | `testpass` |

4. After authentication, you are redirected back to Envoy and the request
   reaches the upstream service (httpbin.org) with SAML identity headers set.

## Makefile Targets

| Target           | Description                                      |
|------------------|--------------------------------------------------|
| `make keycloak-up`   | Start the Keycloak container (foreground)    |
| `make keycloak-down` | Stop the Keycloak container                  |
| `make run`           | Build and run Envoy with the SAML extension  |

## Key URLs

| Resource                | URL                                                             |
|-------------------------|-----------------------------------------------------------------|
| Envoy (protected app)   | http://localhost:10000                                          |
| Keycloak Admin Console  | http://localhost:8080 (`admin` / `admin`)                       |
| Keycloak SAML Metadata  | http://localhost:8080/realms/saml-demo/protocol/saml/descriptor |

## Notes

- Every time Keycloak restarts, its SAML signing keys are regenerated. Delete
  `gen/idp-metadata.xml` (or the whole `gen/` directory) so that `make run`
  downloads the fresh metadata.
- The SP certificate and key are stored in `gen/` and generated once. They are
  git-ignored.
- See [keycloak/README.md](keycloak/README.md) for details on the Keycloak
  setup, test user, and how to customize the SAML client.
