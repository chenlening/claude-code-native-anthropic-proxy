#!/bin/bash
# Patch fastapi uvicorn to use 4 workers. Runs alongside /init,
# waiting for s6 to create the service directory, then patching the run script
# and restarting the service to apply the change.

(
  for i in $(seq 1 300); do
    if [ -f /run/service/fastapi/run ]; then
      if grep -q '\-\-workers' /run/service/fastapi/run 2>/dev/null; then
        exit 0
      fi
      sed -i 's/--port 8765/--port 8765 --workers 4/' /run/service/fastapi/run 2>/dev/null
      # Stop then start fastapi to pick up the patched run script
      sleep 0.5
      /command/s6-svc -t /run/service/fastapi 2>/dev/null
      sleep 1
      /command/s6-svc -u /run/service/fastapi 2>/dev/null
      exit 0
    fi
    sleep 0.01
  done
) &

exec /init
