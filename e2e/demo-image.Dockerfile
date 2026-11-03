# Demo image pushed to the test registry once per version; the healthcheck fails while DEMO_HEALTH=fail.
# The unhealthy target never passes its healthcheck, whatever the stack sets, for versions that must fail.
FROM scratch AS base
ARG VERSION
COPY fakes /fakes
ENV DEMO_NAME=demo DEMO_VERSION=$VERSION
ENTRYPOINT ["/fakes", "demoapp"]

FROM base AS unhealthy
HEALTHCHECK --interval=2s --timeout=3s --start-period=1s --retries=2 CMD ["/fakes", "unhealthy"]

FROM base AS healthy
HEALTHCHECK --interval=2s --timeout=3s --start-period=1s --retries=2 CMD ["/fakes", "healthcheck"]
