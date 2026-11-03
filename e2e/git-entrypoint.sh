#!/bin/sh
# Seeds the demo stacks repository on first start, then runs sshd in the foreground.
set -eu

ssh-keygen -A >/dev/null
install -m 0600 -o git -g git /dev/null /srv/git/.ssh/authorized_keys
printf '%s\n' "$GIT_AUTHORIZED_KEY" >/srv/git/.ssh/authorized_keys

if [ ! -d /srv/git/stacks.git ]; then
	su git -s /bin/sh -c '
		set -eu
		git init -q --bare --initial-branch=main /srv/git/stacks.git
		seed=$(mktemp -d)
		cp -r /usr/local/share/demo-stacks/. "$seed"
		cd "$seed"
		git init -q --initial-branch=main
		git add .
		git -c user.name="E2E Seed" -c user.email=seed.qdeploye2e.test commit -q -m "Initial demo stacks"
		git push -q /srv/git/stacks.git main
		rm -rf "$seed"
	'
fi

exec /usr/sbin/sshd -D -e
