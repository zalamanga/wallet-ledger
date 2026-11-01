FROM postgres:17-alpine
COPY scripts/provision-runtime.sh /tools/provision-runtime.sh
ENTRYPOINT ["/bin/sh", "/tools/provision-runtime.sh"]
