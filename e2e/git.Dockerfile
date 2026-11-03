# Git platform: SSH access limited to git-shell with a key, bare repositories under /srv/git, one seeded demo
# stacks repository.
FROM debian:12-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends git openssh-server \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /run/sshd /srv/git \
	&& useradd --home-dir /srv/git --shell /usr/bin/git-shell git \
	&& usermod -p '*' git \
	&& mkdir -p /srv/git/.ssh \
	&& chown -R git:git /srv/git

COPY git-sshd.conf /etc/ssh/sshd_config.d/qdeploye2e.conf
COPY demo-stacks /usr/local/share/demo-stacks
COPY git-entrypoint.sh /usr/local/bin/git-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/git-entrypoint.sh"]
