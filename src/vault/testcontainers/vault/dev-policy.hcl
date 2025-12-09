# Write/read/update/delete secret at "secret/boo"
path "secret/data/boo" {
  capabilities = ["create", "read", "update", "delete"]
}

# Write/read/update/delete any subpath under "secret/boo/*"
path "secret/data/boo/*" {
  capabilities = ["create", "read", "update", "delete"]
}

# Metadata access needed by KV v2 (e.g. for list)
path "secret/metadata/boo" {
  capabilities = ["list", "delete"]
}

path "secret/metadata/boo/*" {
  capabilities = ["list", "delete"]
}

# Allow reading the specific secret needed by the application
path "secret/data/boo-audit" {
  capabilities = ["read"]
}
