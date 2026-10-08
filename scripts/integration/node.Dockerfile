FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update && apt-get install -y --no-install-recommends openssh-server python3 ca-certificates && rm -rf /var/lib/apt/lists/* && mkdir -p /run/sshd /etc/naive-node /var/lib/naive-node /var/lib/naive-usage /root/.ssh
COPY naive-node /usr/local/bin/naive-node
COPY naive-caddy /usr/local/bin/caddy
COPY node-entrypoint.sh /usr/local/bin/node-entrypoint
COPY echo.py /usr/local/bin/echo.py
COPY node-config.json /etc/naive-node/config.json
COPY Caddyfile.template /etc/naive-node/Caddyfile.template
RUN chmod 755 /usr/local/bin/naive-node /usr/local/bin/caddy /usr/local/bin/node-entrypoint && chmod 700 /root/.ssh && chmod 600 /etc/naive-node/*
ENTRYPOINT ["/usr/local/bin/node-entrypoint"]
