# Local Keycloak SAML IdP

Run a local Keycloak instance pre-configured as a SAML Identity Provider.

## Prerequisites

- Docker and Docker Compose

## Quick Start

```bash
docker compose up
```

Keycloak will be available at **http://localhost:8080** after about 30 seconds.

## What's Pre-Configured

On first start, the `saml-demo` realm is automatically imported with:

### Test User

| Field    | Value                  |
|----------|------------------------|
| Username | `testuser`             |
| Password | `testpass`             |
| Email    | `testuser@example.com` |
| Name     | Test User              |

### SAML Client (`saml-sp`)

| Setting                        | Value                          |
|--------------------------------|--------------------------------|
| Assertion Consumer Service URL | `http://localhost:10000/acs`    |
| Single Logout Service URL      | `http://localhost:10000/slo`    |
| Name ID Format                 | email                          |
| Signature Algorithm            | RSA_SHA256                     |

SAML attribute mappers are included for `email`, `firstName`, and `lastName`.

## Key URLs

| Resource              | URL                                                              |
|-----------------------|------------------------------------------------------------------|
| Admin Console         | http://localhost:8080 (`admin` / `admin`)                        |
| SAML IdP Metadata     | http://localhost:8080/realms/saml-demo/protocol/saml/descriptor  |

## Generating SP Certificates

Your Service Provider may need a key pair to sign SAML requests and decrypt encrypted assertions from the IdP.

### 1. Generate a self-signed certificate and private key

```bash
openssl req -x509 -newkey rsa:2048 -keyout sp-key.pem -out sp-cert.pem -days 3650 -nodes \
  -subj "/CN=saml-sp"
```

This creates two files:

| File          | Purpose                                              |
|---------------|------------------------------------------------------|
| `sp-key.pem`  | Private key — used by your SP to sign requests and decrypt assertions. **Keep this secret.** |
| `sp-cert.pem` | Public certificate — shared with the IdP so it can verify SP signatures and encrypt assertions. |

### 2. Upload the certificate to Keycloak

1. Open the Admin Console at http://localhost:8080 and log in (`admin` / `admin`).
2. Select the **saml-demo** realm.
3. Go to **Clients** > **saml-sp** > **Keys** tab.
4. Click **Import Key**, select **Certificate PEM** as the archive format, and upload `sp-cert.pem`.

### 3. Configure your SP

Point your SP application to the generated key and certificate. For example, in most SAML libraries you will need to provide:

- **SP Private Key**: the contents of `sp-key.pem`
- **SP Certificate**: the contents of `sp-cert.pem`
- **IdP Metadata URL**: `http://localhost:8080/realms/saml-demo/protocol/saml/descriptor`

## Configuring Your Service Provider

Point your SP to the SAML IdP metadata URL above. It contains the SSO endpoint, signing certificate, and other details your SP needs.

If your SP runs on a different host or port than `localhost:10000`, update the SAML client settings either:

- **Before startup**: edit `realm-export.json` and change the `saml_assertion_consumer_url_post`, `saml_single_logout_service_url_redirect`, `redirectUris`, and `baseUrl` values.
- **After startup**: go to the Admin Console > `saml-demo` realm > Clients > `saml-sp` and update the URLs there.

## Stopping

```bash
docker compose down
```
