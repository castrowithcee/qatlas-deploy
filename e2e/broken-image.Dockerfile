# A test image derived from a real one whose healthcheck always fails, for the update that must roll back.
ARG BASE
FROM ${BASE}
LABEL qdeploy.e2e.test-image="derived from the base image; its healthcheck always fails"
HEALTHCHECK --interval=2s --timeout=3s --start-period=1s --retries=1 CMD ["false"]
