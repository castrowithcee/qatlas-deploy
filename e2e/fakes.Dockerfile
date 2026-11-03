# Helper services: GitHub releases fake and webhook receiver (one binary, started per service).
FROM debian:12-slim

RUN mkdir -p /data /srv/releases

COPY fakes /usr/local/bin/fakes
COPY certs /certs
COPY releases /srv/releases

ENTRYPOINT ["/usr/local/bin/fakes"]
