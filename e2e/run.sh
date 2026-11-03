#!/bin/sh
# End-to-end environment: a simulated Linux host (systemd + Docker) with a local git platform (SSH, push), an
# OCI registry with demo image tags, a GitHub releases fake and a webhook receiver.
#
#   e2e/run.sh up                 build everything and start the environment
#   e2e/run.sh down               remove every container, network, volume, image and temp directory it made
#   e2e/run.sh status             show the environment and the registry tags
#   e2e/run.sh exec [-u user] <command...>   run a command on the simulated host (default user e2e-admin)
#   e2e/run.sh env                print the path of the file with the generated test values (source it)
#   e2e/run.sh requests           print the requests the webhook receiver recorded (JSON lines)
#   e2e/run.sh scenario           run the scenario once after up (see e2e/scenario.sh)
#
# On the host, e2e-admin reaches the stacks repo with: git clone git.qdeploye2e.test:stacks.git
# The registry is registry.qdeploye2e.test (image demo/app, tags 1.0.0 1.0.1 1.1.0 2.0.0 2.1.0-rc.1); a demo
# container turns unhealthy with DEMO_HEALTH=fail.
#
# Everything is named qdeploye2e-* and labelled qdeploy.e2e=1; down touches only what carries both. Nothing
# is published on the machine's ports. Set E2E_KEEP_ON_FAIL=1 to keep a failed environment for inspection.
# Needs docker (rootless is fine), go, openssl, ssh-keygen.
set -eu

prefix=qdeploye2e
label=qdeploy.e2e=1
domain=qdeploye2e.test
hostname_=qdeploy-e2e-host
admin=e2e-admin
net=$prefix-net
host=$prefix-host
tags='1.0.0 1.0.1 1.1.0 2.0.0 2.1.0-rc.1'
backup_image=$prefix-backup-helper:1

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
tmp_base=${TMPDIR:-/tmp}

fail() {
	printf 'e2e: %s\n' "$*" >&2
	exit 1
}

log() {
	printf 'e2e: %s\n' "$*"
}

# owned lists "<id> <name>" of the objects of one kind whose name starts with the prefix and that carry the label.
owned() {
	case $1 in
	container) docker ps -a --filter "label=$label" --format '{{.ID}} {{.Names}}' ;;
	network) docker network ls --filter "label=$label" --format '{{.ID}} {{.Name}}' ;;
	volume) docker volume ls --filter "label=$label" --format '{{.Name}} {{.Name}}' ;;
	image) docker images --filter "label=$label" --format '{{.ID}} {{.Repository}}:{{.Tag}}' ;;
	esac | awk -v p="$prefix-" 'index($2, p) == 1'
}

# named lists the objects of one kind by prefix alone, to find leftovers that lost their label.
named() {
	case $1 in
	container) docker ps -a --format '{{.Names}}' ;;
	network) docker network ls --format '{{.Name}}' ;;
	volume) docker volume ls --format '{{.Name}}' ;;
	image) docker images --format '{{.Repository}}:{{.Tag}}' ;;
	esac | awk -v p="$prefix" 'index($0, p) == 1'
}

workdirs() {
	for dir in "$tmp_base"/$prefix.*; do
		[ -d "$dir" ] && [ -O "$dir" ] && [ -f "$dir/.$prefix" ] && printf '%s\n' "$dir"
	done
	return 0
}

current_work() {
	workdirs | head -n 1
}

require_up() {
	work=$(current_work)
	[ -n "$work" ] || fail 'the environment is not up; run: e2e/run.sh up'
}

down() {
	log 'removing containers, networks, volumes, images and temp directories'
	owned container | while read -r id name; do
		docker rm -f -v "$id" >/dev/null && log "removed container $name"
	done
	owned network | while read -r id name; do
		docker network rm "$id" >/dev/null && log "removed network $name"
	done
	owned volume | while read -r id name; do
		docker volume rm "$id" >/dev/null && log "removed volume $name"
	done
	# Removed by tag, not by ID: only our tag goes, never an image that happens to share content.
	owned image | while read -r _ name; do
		docker rmi "$name" >/dev/null && log "removed image $name"
	done
	workdirs | while read -r dir; do
		rm -rf "$dir" && log "removed $dir"
	done

	left=
	for kind in container network volume image; do
		found=$(named "$kind")
		if [ -n "$found" ]; then
			left="$left $kind:$(printf '%s' "$found" | tr '\n' ',')"
		fi
	done
	[ -z "$(workdirs)" ] || left="$left tmp-dir"
	[ -z "$left" ] || fail "leftovers with the prefix $prefix:$left"
	log "clean: nothing with the prefix $prefix remains"
}

make_keys_and_certs() {
	mkdir -p "$work/keys" "$work/ctx/certs" "$work/ctx/releases"
	# One key pair for the administrator on the simulated host; the git platform accepts only this public key.
	ssh-keygen -q -t ed25519 -N '' -C "$prefix-test-key" -f "$work/keys/admin"

	ca=$work/ctx/certs
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 2 \
		-subj "/CN=$prefix test CA" -keyout "$work/keys/ca.key" -out "$ca/ca.crt" 2>/dev/null
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
		-subj "/CN=$prefix helper services" -keyout "$ca/server.key" -out "$work/keys/server.csr" 2>/dev/null
	cat >"$work/keys/server.ext" <<EXT
subjectAltName = DNS:registry.$domain, DNS:hooks.$domain, DNS:releases.$domain, DNS:api.github.com, DNS:github.com
extendedKeyUsage = serverAuth
EXT
	openssl x509 -req -in "$work/keys/server.csr" -CA "$ca/ca.crt" -CAkey "$work/keys/ca.key" -CAcreateserial \
		-days 2 -extfile "$work/keys/server.ext" -out "$ca/server.crt" 2>/dev/null
}

write_testenv() {
	cat >"$work/testenv" <<ENV
E2E_WORK=$work
E2E_HOST_CONTAINER=$host
E2E_HOST_ADMIN=$admin
E2E_HOSTNAME=$hostname_
E2E_CA_CERT=$work/ctx/certs/ca.crt
E2E_SSH_KEY=$work/keys/admin
E2E_GIT_URL=git.$domain:stacks.git
E2E_REGISTRY=registry.$domain
E2E_IMAGE=registry.$domain/demo/app
E2E_TAGS='$tags'
E2E_BACKUP_IMAGE=$backup_image
E2E_RELEASES_URL=https://releases.$domain
E2E_GITHUB_API=https://api.github.com
E2E_WEBHOOK_URL=https://hooks.$domain/hook
E2E_CHANGELOG_IMAGE_REPO=qdeploye2e-org/demo-image
E2E_CHANGELOG_GIT_REPO=qdeploye2e-org/demo-stacks
ENV
}

build_images() {
	log 'building the fakes'
	(cd "$root" && CGO_ENABLED=0 go build -tags e2efakes -trimpath -o "$work/ctx/fakes" ./e2e/fakes)
	cp -r "$here/demo-stacks" "$here/git-sshd.conf" "$here/git-entrypoint.sh" "$work/ctx/"

	log 'building the images (the simulated host takes a while on the first run)'
	docker build -q --label "$label" -t "$prefix-fakes:latest" -f "$here/fakes.Dockerfile" "$work/ctx" >/dev/null
	docker build -q --label "$label" -t "$prefix-registry:latest" -f "$here/registry.Dockerfile" "$work/ctx" >/dev/null
	docker build -q --label "$label" -t "$prefix-git:latest" -f "$here/git.Dockerfile" "$work/ctx" >/dev/null
	docker build -q --label "$label" -t "$prefix-host:latest" - <"$here/host.Dockerfile" >/dev/null
}

start_services() {
	docker network create --label "$label" "$net" >/dev/null
	docker volume create --label "$label" "$prefix-docker" >/dev/null
	docker volume create --label "$label" "$prefix-containerd" >/dev/null
	docker volume create --label "$label" "$prefix-registry" >/dev/null
	docker volume create --label "$label" "$prefix-git" >/dev/null

	docker run -d --name "$prefix-registry" --label "$label" --network "$net" \
		--network-alias "registry.$domain" -v "$prefix-registry:/var/lib/registry" "$prefix-registry:latest" >/dev/null
	docker run -d --name "$prefix-git" --label "$label" --network "$net" --network-alias "git.$domain" \
		-e GIT_AUTHORIZED_KEY="$(cat "$work/keys/admin.pub")" -v "$prefix-git:/srv/git" "$prefix-git:latest" >/dev/null
	docker run -d --name "$prefix-hooks" --label "$label" --network "$net" --network-alias "hooks.$domain" \
		"$prefix-fakes:latest" webhook >/dev/null
	docker run -d --name "$prefix-releases" --label "$label" --network "$net" \
		--network-alias "releases.$domain" --network-alias api.github.com --network-alias github.com \
		"$prefix-fakes:latest" releases >/dev/null

	docker run -d --name "$host" --label "$label" --hostname "$hostname_" --network "$net" \
		--privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock \
		-v "$prefix-docker:/var/lib/docker" -v "$prefix-containerd:/var/lib/containerd" \
		"$prefix-host:latest" >/dev/null
}

wait_for() {
	what=$1 tries=$2
	shift 2
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt "$tries" ] || fail "timeout waiting for $what"
		sleep 1
	done
}

host_booted() {
	state=$(docker exec "$host" systemctl is-system-running 2>/dev/null || true)
	[ "$state" = running ] || [ "$state" = degraded ]
}

git_host_key() {
	docker exec "$prefix-git" cat /etc/ssh/ssh_host_ed25519_key.pub
}

prepare_host() {
	wait_for 'the host to boot' 90 host_booted
	docker cp "$work/ctx/certs/ca.crt" "$host:/usr/local/share/ca-certificates/$prefix-ca.crt"
	docker exec "$host" update-ca-certificates >/dev/null
	# The inner Docker daemon reads per-registry CAs without a restart.
	docker exec "$host" mkdir -p "/etc/docker/certs.d/registry.$domain"
	docker cp "$work/ctx/certs/ca.crt" "$host:/etc/docker/certs.d/registry.$domain/ca.crt"
	wait_for 'the inner Docker daemon' 60 docker exec "$host" docker info

	wait_for 'the git host key' 30 git_host_key
	ssh_dir=$work/ssh-admin
	mkdir -p "$ssh_dir"
	cp "$work/keys/admin" "$ssh_dir/id_ed25519"
	printf 'Host git.%s\n\tUser git\n\tIdentityFile /home/%s/.ssh/id_ed25519\n' "$domain" "$admin" >"$ssh_dir/config"
	printf 'git.%s %s\n' "$domain" "$(git_host_key | cut -d ' ' -f 1,2)" >"$ssh_dir/known_hosts"
	docker cp "$ssh_dir" "$host:/home/$admin/.ssh"
	docker exec "$host" sh -c "chown -R $admin:$admin /home/$admin/.ssh && chmod 700 /home/$admin/.ssh && chmod 600 /home/$admin/.ssh/id_ed25519"
	rm -rf "$ssh_dir"
}

# push_images builds the demo image once per tag inside the host and pushes it to the registry.
push_images() {
	log "pushing the demo image tags: $tags"
	docker exec "$host" mkdir -p /tmp/demo-image
	docker cp "$work/ctx/fakes" "$host:/tmp/demo-image/fakes"
	docker cp "$here/demo-image.Dockerfile" "$host:/tmp/demo-image/Dockerfile"
	for tag in $tags; do
		docker exec -w /tmp/demo-image "$host" sh -ec "
			docker build -q --build-arg VERSION=$tag -t registry.$domain/demo/app:$tag . >/dev/null
			docker push -q registry.$domain/demo/app:$tag >/dev/null
			docker rmi registry.$domain/demo/app:$tag >/dev/null"
	done
	docker exec "$host" rm -rf /tmp/demo-image
}

# load_backup_helper gives the host the image of the backup helper containers. The host has no internet, so the
# Debian base of the host image (GNU tar, find, rm, sh) is copied in from the machine's Docker.
load_backup_helper() {
	log "loading the backup helper image $backup_image"
	docker image inspect debian:12 >/dev/null 2>&1 || fail 'the base image debian:12 is not available'
	docker save debian:12 | docker exec -i "$host" docker load -q >/dev/null
	docker exec "$host" sh -ec "docker tag debian:12 $backup_image && docker rmi debian:12 >/dev/null"
}

# http_code prints the status code the host gets for a URL.
http_code() {
	docker exec "$host" curl -sS -o /dev/null -w '%{http_code}' "$@"
}

check_services() {
	log 'checking the helper services from the host'
	api=$(docker exec "$host" curl -fsS https://api.github.com/repos/castrowithcee/qatlas-deploy/releases) ||
		fail 'the releases fake is not reachable over trusted HTTPS'
	[ -n "$api" ] || fail 'the releases fake answered nothing'
	got=$(docker exec "$host" curl -fsS "https://registry.$domain/v2/demo/app/tags/list") ||
		fail 'the registry is not reachable over trusted HTTPS'
	for tag in $tags; do
		printf '%s' "$got" | grep -q "\"$tag\"" || fail "the registry does not list the tag $tag"
	done
	[ "$(http_code -X POST -d '{"check":true}' "https://hooks.$domain/hook")" = 204 ] ||
		fail 'the webhook receiver does not accept a request'
	docker exec "$host" curl -fsS "https://hooks.$domain/_e2e/requests" | grep -q '"check\\":true' ||
		fail 'the webhook receiver did not record the request'
	docker exec -u "$admin" "$host" ssh -o BatchMode=yes "git.$domain" 2>&1 | grep -qi 'interactive git shell is not enabled' ||
		fail 'the git server does not answer over SSH'
}

up() {
	for tool in docker go openssl ssh-keygen; do
		command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
	done
	[ -z "$(named container)$(named network)$(named volume)$(named image)$(workdirs)" ] ||
		fail "something with the prefix $prefix exists already; run: e2e/run.sh down"

	work=$(mktemp -d "$tmp_base/$prefix.XXXXXX")
	: >"$work/.$prefix"
	done_=
	trap 'if [ -z "$done_" ]; then
		if [ -n "${E2E_KEEP_ON_FAIL:-}" ]; then printf "e2e: up failed; environment kept, remove it with: e2e/run.sh down\n" >&2
		else printf "e2e: up failed; tearing down\n" >&2; down || true; fi
	fi' EXIT
	trap 'exit 1' HUP INT TERM

	make_keys_and_certs
	write_testenv
	build_images
	start_services
	prepare_host
	push_images
	load_backup_helper
	check_services
	done_=1
	log "up; test values: $work/testenv"
}

status() {
	require_up
	owned container | while read -r _ name; do
		printf '%s %s\n' "$name" "$(docker inspect -f '{{.State.Status}}' "$name")"
	done
	printf 'registry tags: %s\n' "$(docker exec "$host" curl -fsS "https://registry.$domain/v2/demo/app/tags/list" 2>/dev/null || true)"
	printf 'test values: %s\n' "$work/testenv"
}

main() {
	cmd=${1:-}
	[ $# -eq 0 ] || shift
	case $cmd in
	up) up ;;
	down) down ;;
	status) status ;;
	env)
		require_up
		printf '%s\n' "$work/testenv"
		;;
	requests)
		require_up
		docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null || true
		;;
	scenario)
		require_up
		. "$here/scenario.sh"
		run_scenario
		;;
	exec)
		require_up
		user=$admin
		if [ "${1:-}" = -u ]; then
			[ $# -ge 2 ] || fail 'exec -u needs a user'
			user=$2
			shift 2
		fi
		[ $# -gt 0 ] || fail 'exec needs a command'
		if [ -t 0 ]; then
			exec docker exec -it -u "$user" "$host" "$@"
		fi
		exec docker exec -i -u "$user" "$host" "$@"
		;;
	*)
		sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'
		[ -z "$cmd" ] || exit 2
		;;
	esac
}

main "$@"
