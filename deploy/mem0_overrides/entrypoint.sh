#!/bin/bash
# Patch uvicorn to use multiple workers before s6 starts services.
# s6-overlay creates /run/service/fastapi/run at init time,
# so we hook into the init process via /etc/s6-overlay/s6-rc.d.

# Create a oneshot fixup service that runs before fastapi
mkdir -p /etc/s6-overlay/s6-rc.d/fixup-fastapi/contents.d
cat > /etc/s6-overlay/s6-rc.d/fixup-fastapi/type << 'TYPE'
oneshot
TYPE
cat > /etc/s6-overlay/s6-rc.d/fixup-fastapi/contents.d/up << 'UP'
#!/bin/bash
sed -i 's/--port 8765/--port 8765 --workers 4/' /run/service/fastapi/run 2>/dev/null || true
UP
chmod +x /etc/s6-overlay/s6-rc.d/fixup-fastapi/contents.d/up

exec /init
