# Base images of the e2e environment: everything that needs the internet to build. e2e/run.sh deps builds one
# image per target, tagged by the hash of this file; up builds on them without network.

# Simulated Linux host: systemd as init, Docker CE, and the tools an administrator expects.
FROM debian:12 AS host

ENV container=docker DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
	&& apt-get install -y --no-install-recommends systemd systemd-sysv dbus ca-certificates curl tmux git \
		openssh-client openssh-server sudo \
	&& install -m 0755 -d /etc/apt/keyrings \
	&& curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
	&& echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" \
		>/etc/apt/sources.list.d/docker.list \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-compose-plugin \
	&& rm -rf /var/lib/apt/lists/* \
	&& systemctl enable docker containerd ssh

# The test registry mirrors Docker Hub for the images the stacks and qdeploy pull by their Hub names (the backup
# helper busybox by digest); the host has no other way to reach Docker Hub.
RUN mkdir -p /etc/docker \
	&& echo '{"registry-mirrors": ["https://registry.qdeploye2e.test"]}' >/etc/docker/daemon.json

# An administrator account the way a real host has one.
RUN useradd --create-home --shell /bin/bash --groups sudo e2e-admin \
	&& echo 'e2e-admin ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/e2e-admin \
	&& chmod 0440 /etc/sudoers.d/e2e-admin

# systemd leaves mount propagation to the container manager, and Docker makes every mount private. A real host
# has shared mounts, which systemd needs to pass credentials to a service (LoadCredentialEncrypted).
STOPSIGNAL SIGRTMIN+3
CMD ["/bin/sh", "-c", "mount --make-rshared / && exec /sbin/init"]

# Git platform: SSH access limited to git-shell with a key (account git, and qdeploy as a second name for it so
# the service reaches the repository by a URL without a user part), bare repositories under /srv/git.
FROM debian:12-slim AS git

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

# Copies real images from the internet into the test registry for the acceptance scenario (skopeo talks to
# registries directly, so the machine's Docker daemon never stores them).
FROM debian:12-slim AS skopeo

RUN apt-get update \
	&& apt-get install -y --no-install-recommends skopeo ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
