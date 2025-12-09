#!/bin/sh

echo "Vault is up. Configuring..."

# Enable userpass auth
vault auth enable userpass

# Create policy
vault policy write dev-policy /dev-policy.hcl

# Create user with that policy
vault write auth/userpass/users/boo-user password="boo-password" policies="dev-policy"

# load cert data
CERT_DATA=$(cat /dev-cert.pem | base64)

# Store secret data
vault kv put secret/boo-audit requestAuditCert="$CERT_DATA" requestAuditCertPass="password"

echo "Vault configuration complete."