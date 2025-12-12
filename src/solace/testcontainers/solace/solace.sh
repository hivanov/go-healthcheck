#!/bin/bash
set -x
# This script is executed automatically after the Solace broker starts.

echo "--- DIAGNOSTIC: post-startup.sh started at $(date) ---"

echo "--- Waiting for server start ---"
until curl -f http://localhost:8080; do
  sleep 5
done

# CLI readiness loop removed as per user request.

for i in $(seq 1 10); do
  echo "--- Attempt $i to enable SSL --- "
  /usr/sw/loads/currentload/bin/cli -A -s /usr/sw/jail/cliscripts/solace_config.cli
  RETVAL=$?

  echo "--- DIAGNOSTIC: Solace CLI command exit code: $RETVAL ---"
  if [ $RETVAL -eq 0 ]; then
    break
  fi
  sleep 5
done

if [ $RETVAL -eq 0 ]; then
  echo "--- Post-startup script finished successfully ---"
  touch /tmp/solace_configured.ready
else
  echo "--- Post-startup script finished with error code $RETVAL ---"
  touch /tmp/solace_coonfigured.failure
fi
exit $RETVAL