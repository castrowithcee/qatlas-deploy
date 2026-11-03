# Git platform: SSH access limited to git-shell with a key (account git, and qdeploy as a second name for it so
# the service reaches the repository by a URL without a user part), bare repositories under /srv/git, one seeded demo
# stacks repository.
FROM debian:12-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends git openssh-server \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /run/sshd /srv/git \
	&& useradd --home-dir /srv/git --shell /usr/bin/git-shell git \
	&& usermod -p '*' git \
	&& useradd -o -u "$(id -u git)" -g git --home-dir /srv/git --no-create-home --shell /usr/bin/git-shell qdeploy \
	&& usermod -p '*' qdeploy \
	&& mkdir -p /srv/git/.ssh \
	&& chown -R git:git /srv/git

COPY git-sshd.conf /etc/ssh/sshd_config.d/qdeploye2e.conf
COPY demo-stacks /usr/local/share/demo-stacks
COPY git-entrypoint.sh /usr/local/bin/git-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/git-entrypoint.sh"]
