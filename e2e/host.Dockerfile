# Simulated Linux host: systemd as init, Docker CE, and the tools an administrator expects.
FROM debian:12

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

# An administrator account the way a real host has one.
RUN useradd --create-home --shell /bin/bash --groups sudo e2e-admin \
	&& echo 'e2e-admin ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/e2e-admin \
	&& chmod 0440 /etc/sudoers.d/e2e-admin

# systemd leaves mount propagation to the container manager, and Docker makes every mount private. A real host
# has shared mounts, which systemd needs to pass credentials to a service (LoadCredentialEncrypted).
STOPSIGNAL SIGRTMIN+3
CMD ["/bin/sh", "-c", "mount --make-rshared / && exec /sbin/init"]
