# Grafana LDAP Configuration JSONSchema

This JSONSchema validates Grafana LDAP configuration files in TOML format after conversion to YAML.

## Overview

The schema validates Grafana's LDAP authentication configuration, ensuring:

- Required fields are present
- Data types are correct
- Values conform to expected formats and constraints
- Group mappings have valid role assignments

## Schema File

`grafana-ldap-schema.json`

## Resource Type

The schema validates resources with `configSchema: GrafanaLDAP`.

## Configuration Structure

### Top-Level Structure

```toml
[configHub]
configName = LDAPConfig
configSchema = GrafanaLDAP

[configHub.kubernetes]
namespace = production

[[servers]]
# Server configuration...

[[servers.group_mappings]]
# Group mappings...
```

When converted to YAML, this becomes:

```yaml
configHub:
  configName: LDAPConfig
  configSchema: GrafanaLDAP
  kubernetes:
    namespace: production
servers:
  - host: ldap.example.com
    port: 636
    # ...
    group_mappings:
      - group_dn: "*"
        org_role: Viewer
```

## Required Sections

### 1. `configHub` (Required)

Metadata section for ConfigHub management.

**Required Fields:**

- `configSchema`: Must be `"GrafanaLDAP"`

**Optional Fields:**

- `configName`: Name of the configuration
- `kubernetes.namespace`: Kubernetes namespace for deployment

### 2. `servers` (Required)

Array of LDAP servers. At least one server must be configured.

#### Server Connection Properties

**Required:**

- `host` (string): LDAP server hostname
- `bind_dn` (string): Distinguished name for binding
- `search_filter` (string): User search filter with `%s` placeholder
- `search_base_dns` (array): Base DNs for user searches

**Optional:**

- `port` (integer, 1-65535): Server port (default: 389 or 636)
- `use_ssl` (boolean): Enable LDAPS (default: false)
- `start_tls` (boolean): Use STARTTLS (default: false)
- `ssl_skip_verify` (boolean): Skip cert verification (default: false)
- `root_ca_cert` (string): Path to CA certificate
- `client_cert` (string): Path to client certificate
- `client_key` (string): Path to client key
- `tls_ciphers` (array): Accepted TLS ciphers
- `min_tls_version` (string): "TLS1.2" or "TLS1.3"
- `timeout` (integer): Connection timeout in seconds
- `bind_password` (string): Password for bind DN

#### Server Attributes (Required)

Mapping of LDAP attributes to Grafana user fields.

**Required:**

- `username`: LDAP attribute for username
- `email`: LDAP attribute for email
- `member_of`: LDAP attribute for group membership

**Optional:**

- `name`: Display name attribute
- `surname`: Surname attribute

Example:

```toml
[servers.attributes]
username  = sAMAccountName
email     = mail
member_of = memberOf
name      = givenName
surname   = sn
```

#### Group Mappings (Optional)

Maps LDAP groups to Grafana organization roles.

**Required:**

- `group_dn` (string): LDAP group DN or "\*" for wildcard
- `org_role` (string): One of "Admin", "Editor", or "Viewer"

**Optional:**

- `org_id` (integer): Organization ID (default: 1)
- `grafana_admin` (boolean): Grant super admin (default: false)

Example:

```toml
[[servers.group_mappings]]
group_dn      = CN=grafana-admins,OU=groups,DC=example,DC=com
org_role      = Admin
grafana_admin = false

[[servers.group_mappings]]
group_dn = *
org_role = Viewer
```

### 3. `auth.ldap` (Optional)

Main LDAP authentication settings for Grafana.

**Optional Fields:**

- `enabled` (boolean): Enable LDAP auth
- `config_file` (string): Path to LDAP config
- `allow_sign_up` (boolean): Auto-create users
- `skip_org_role_sync` (boolean): Skip role sync

## Validation Examples

### Valid Configuration

```toml
[[servers]]
host            = ldap.example.com
port            = 636
bind_dn         = CN=admin,DC=example,DC=com
bind_password   = secret
search_filter   = (&(objectClass=user)(sAMAccountName=%s))
search_base_dns = ["DC=example,DC=com"]
use_ssl         = true
ssl_skip_verify = false

[servers.attributes]
username  = sAMAccountName
email     = mail
member_of = memberOf

[[servers.group_mappings]]
group_dn = *
org_role = Viewer

[configHub]
configSchema = GrafanaLDAP
```

### Invalid Configuration Examples

#### Missing Required Fields

```toml
[[servers]]
host = ldap.example.com
# Missing: bind_dn, search_filter, search_base_dns

[configHub]
configSchema = GrafanaLDAP
```

Error: Required fields missing in servers configuration.

#### Invalid Port

```toml
[[servers]]
host            = ldap.example.com
port            = 99999  # Invalid: exceeds maximum
bind_dn         = CN=admin,DC=example,DC=com
search_filter   = (cn=%s)
search_base_dns = ["DC=example,DC=com"]

[servers.attributes]
username  = cn
email     = mail
member_of = memberOf

[configHub]
configSchema = GrafanaLDAP
```

Error: Port must be between 1 and 65535.

#### Invalid Role

```toml
[[servers]]
host            = ldap.example.com
bind_dn         = CN=admin,DC=example,DC=com
search_filter   = (cn=%s)
search_base_dns = ["DC=example,DC=com"]

[servers.attributes]
username  = cn
email     = mail
member_of = memberOf

[[servers.group_mappings]]
group_dn = *
org_role = SuperUser  # Invalid: not one of Admin/Editor/Viewer

[configHub]
configSchema = GrafanaLDAP
```

Error: org_role must be one of: Admin, Editor, Viewer.

## Using the Schema

### With ConfigHub Function

```bash
# Validate Grafana LDAP configuration
fctl do --toolchain "AppConfig/TOML" \
  grafana.toml \
  "GrafanaLDAP" \
  vet-jsonschema \
  "$(cat grafana-ldap-schema.json)"
```

## Variable Expansion

The schema supports variable expansion in string fields:

- `$__env{VAR_NAME}`: Expands to environment variable value
- Example: `bind_dn = $__env{LDAP_ADMIN_DN}`

## References

- [Grafana LDAP Documentation](https://grafana.com/docs/grafana/latest/setup-grafana/configure-access/configure-authentication/ldap/)
- JSONSchema Draft 7: http://json-schema.org/draft-07/schema#

## Schema Version

- Version: 1.0
- Last Updated: 2025
- Compatible with: Grafana v8.0+
