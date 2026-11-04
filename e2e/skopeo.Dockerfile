# Copies real images from the internet into the test registry (skopeo talks to registries directly, so the
# machine's Docker daemon never stores them).
FROM debian:12-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends skopeo ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

COPY certs/ca.crt /usr/local/share/ca-certificates/qdeploye2e-ca.crt
RUN update-ca-certificates
