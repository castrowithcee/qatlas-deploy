# Git platform of one environment: the base image from deps with the SSH settings and two seeded demo stacks
# repositories.
ARG BASE
FROM ${BASE}

COPY git-sshd.conf /etc/ssh/sshd_config.d/qdeploye2e.conf
COPY demo-stacks /usr/local/share/demo-stacks
COPY demo-stacks-second /usr/local/share/demo-stacks-second
COPY git-entrypoint.sh /usr/local/bin/git-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/git-entrypoint.sh"]
