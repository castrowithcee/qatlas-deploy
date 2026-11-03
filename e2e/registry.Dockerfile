# Image registry with HTTPS; the simulated network is closed, so it has no authentication.
FROM registry:2

COPY certs /certs

ENV REGISTRY_HTTP_ADDR=0.0.0.0:443 \
	REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.crt \
	REGISTRY_HTTP_TLS_KEY=/certs/server.key
