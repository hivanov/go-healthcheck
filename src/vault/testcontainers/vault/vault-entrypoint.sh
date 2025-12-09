#!/bin/sh

# Start Vault server in the background
/usr/local/bin/docker-entrypoint.sh server -dev &

# Wait for Vault to become ready
echo "Waiting for Vault to start..."
until curl --silent --fail http://127.0.0.1:8200/v1/sys/health; do
  sleep 1
done

echo "Vault is up. Running vault-init script..."
/vault-init.sh

# Wait for background Vault process to keep container alive
wait
