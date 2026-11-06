# Git platform of one environment: the base image from deps with the SSH settings and three seeded demo stacks
# repositories.
ARG BASE
FROM ${BASE}

COPY git-sshd.conf /etc/ssh/sshd_config.d/qdeploye2e.conf
COPY demo-stacks /usr/local/share/demo-stacks
COPY demo-stacks-second /usr/local/share/demo-stacks-second
COPY demo-stacks-root /usr/local/share/demo-stacks-root
COPY git-entrypoint.sh /usr/local/bin/git-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/git-entrypoint.sh"]
