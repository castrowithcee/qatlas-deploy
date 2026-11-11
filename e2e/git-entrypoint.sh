#!/bin/sh
# Seeds the demo stacks repository on first start, then runs sshd in the foreground.
set -eu

ssh-keygen -A >/dev/null
install -m 0600 -o git -g git /dev/null /srv/git/.ssh/authorized_keys
printf '%s\n' "$GIT_AUTHORIZED_KEY" >/srv/git/.ssh/authorized_keys

# seed creates the bare repository $1.git from the directory $2 once.
seed_repo() {
	[ ! -d "/srv/git/$1.git" ] || return 0
	su git -s /bin/sh -c '
		set -eu
		git init -q --bare --initial-branch=main "/srv/git/$1.git"
		seed=$(mktemp -d)
		cp -r "$2/." "$seed"
		cd "$seed"
		git init -q --initial-branch=main
		git add .
		git -c user.name="E2E Seed" -c user.email=seed.qdeploye2e.test commit -q -m "Initial demo stacks"
		git push -q "/srv/git/$1.git" main
		rm -rf "$seed"
	' sh "$1" "$2"
}

# empty_repo creates the bare repository $1.git without a commit once.
empty_repo() {
	[ ! -d "/srv/git/$1.git" ] || return 0
	su git -s /bin/sh -c 'git init -q --bare --initial-branch=main "/srv/git/$1.git"' sh "$1"
}

seed_repo stacks /usr/local/share/demo-stacks
# A second repository for the second stack folder: one stack named like one of the first, one of its own.
seed_repo stacks2 /usr/local/share/demo-stacks-second
# A third repository whose root holds the compose file: the whole folder is one stack.
seed_repo stacks3 /usr/local/share/demo-stacks-root
# Two repositories without a commit for the folders that get a remote: one receives a push, the other must stay
# empty because the push to it is refused.
empty_repo stacks4
empty_repo stacks5

exec /usr/sbin/sshd -D -e
