# Demo image pushed to the test registry once per version; the healthcheck fails while DEMO_HEALTH=fail.
FROM scratch
ARG VERSION
COPY fakes /fakes
ENV DEMO_NAME=demo DEMO_VERSION=$VERSION
HEALTHCHECK --interval=2s --timeout=3s --start-period=1s --retries=2 CMD ["/fakes", "healthcheck"]
ENTRYPOINT ["/fakes", "demoapp"]
