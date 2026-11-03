#!/bin/sh
# Scenario: from the simulated host, clone the demo stacks repository over SSH and push a commit; then check
# the demo image tags and that a demo container turns unhealthy on purpose. Then run qdeploy on the host as a
# service, bind the stacks repository with its own deploy key, list the stacks and refuse a changed host key of
# the git server. Sourced by run.sh scenario.

as_admin() {
	docker exec -i -u "$admin" "$host" sh -ec "$*"
}

step() {
	log "scenario: $*"
}

run_scenario() {
	repo=git.$domain:stacks.git

	step 'clone over SSH'
	as_admin "rm -rf ~/stacks && git clone -q $repo ~/stacks"
	as_admin 'cd ~/stacks && test -f demo-stack/compose.yaml && test -f demo-stack/.env && grep -qx "secrets.env" demo-stack/.gitignore && grep -qx "data/" demo-stack/.gitignore' ||
		fail 'the demo repository does not have the expected stack files'
	as_admin 'cd ~/stacks && grep -q "^DEMO_APP_VERSION=" demo-stack/.env && ! git ls-files | grep -q secrets.env' ||
		fail 'the demo repository does not hold the image version as a variable or tracks a secrets file'

	step 'commit and push'
	as_admin 'cd ~/stacks &&
		sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.0.1/" demo-stack/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Update demo-stack to 1.0.1" &&
		git push -q origin main'
	local_head=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	remote_head=$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)
	[ "$local_head" = "$remote_head" ] || fail "the pushed commit is not on the server ($local_head vs $remote_head)"
	as_admin "rm -rf ~/stacks2 && git clone -q $repo ~/stacks2 && grep -qx DEMO_APP_VERSION=1.0.1 ~/stacks2/demo-stack/.env" ||
		fail 'a fresh clone does not see the pushed commit'

	step 'the git server offers no shell'
	if as_admin "ssh -o BatchMode=yes git.$domain id" >/dev/null 2>&1; then
		fail 'the git server ran an arbitrary command'
	fi

	step 'registry tags'
	got=$(as_admin "curl -fsS https://registry.$domain/v2/demo/app/tags/list")
	log "scenario: tags $got"
	for tag in $tags; do
		printf '%s' "$got" | grep -q "\"$tag\"" || fail "the registry does not list $tag"
	done

	step 'demo healthcheck turns unhealthy on purpose'
	image=registry.$domain/demo/app:1.0.0
	docker exec "$host" docker rm -f "$prefix-demo-ok" "$prefix-demo-bad" >/dev/null 2>&1 || true
	docker exec "$host" docker run -d --name "$prefix-demo-ok" "$image" >/dev/null
	docker exec "$host" docker run -d --name "$prefix-demo-bad" -e DEMO_HEALTH=fail "$image" >/dev/null
	wait_for 'the healthy demo container' 30 health_is "$prefix-demo-ok" healthy
	wait_for 'the unhealthy demo container' 30 health_is "$prefix-demo-bad" unhealthy
	log "scenario: $prefix-demo-ok healthy, $prefix-demo-bad unhealthy"
	docker exec "$host" docker rm -f "$prefix-demo-ok" "$prefix-demo-bad" >/dev/null

	scenario_stacks_repo

	log 'scenario: passed'
}

# qd runs the qdeploy CLI on the host as root; stderr is merged so a failure message can be checked.
qd() {
	docker exec -i "$host" qdeploy "$@" 2>&1 </dev/null
}

# qd_fails runs qd, requires a non-zero exit and prints the output.
qd_fails() {
	if out=$(qd "$@"); then
		printf '%s\n' "$out"
		return 1
	fi
	printf '%s\n' "$out"
}

known_hosts_sum() {
	docker exec "$host" sh -c 'sha256sum /var/lib/qdeploy/known_hosts 2>/dev/null || echo none'
}

git_ed25519_fingerprint() {
	docker exec "$prefix-git" ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub | cut -d ' ' -f 2
}

# scanned_git_key prints the ed25519 host key the git server offers now.
scanned_git_key() {
	docker exec -u "$admin" "$host" ssh-keyscan -t ed25519 "git.$domain" 2>/dev/null | grep -v "^#" | cut -d ' ' -f 2,3
}

setup_service() {
	step 'install qdeploy as a test-only service on the host'
	(cd "$root" && GOOS=linux GOARCH="$(docker exec "$host" dpkg --print-architecture)" CGO_ENABLED=0 \
		go build -trimpath -o "$work/qdeploy" .) || fail 'cannot build qdeploy'
	docker cp "$work/qdeploy" "$host:/usr/local/bin/qdeploy"
	docker exec "$host" sh -ec '
		useradd --system --home-dir /var/lib/qdeploy --no-create-home --shell /usr/sbin/nologin qdeploy
		install -d -o qdeploy -g qdeploy -m 0755 /opt/stacks
		systemd-run --quiet --unit=qdeploy-e2e --property=Type=exec --property=User=qdeploy --property=Group=qdeploy \
			--property=StateDirectory=qdeploy --property=StateDirectoryMode=0700 \
			--property=RuntimeDirectory=qdeploy --property=RuntimeDirectoryMode=0750 /usr/local/bin/qdeploy run' ||
		fail 'cannot start the qdeploy service'
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	log 'scenario: qdeploy service running'
}

scenario_stacks_repo() {
	setup_service
	url=git.$domain:stacks.git

	step 'deploy key'
	out=$(qd --output json key generate) || fail "key generate failed: $out"
	pub=$(printf '%s\n' "$out" | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	case $pub in ssh-ed25519\ *) ;; *) fail "no public key in: $out" ;; esac
	printf '%s\n' "$pub" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	log 'scenario: deploy key registered on the git server'

	step 'repo set refuses an unconfirmed host key without a terminal'
	before=$(known_hosts_sum)
	out=$(qd_fails repo set "$url") || fail "repo set without terminal and fingerprint succeeded: $out"
	printf '%s' "$out" | grep -q -- '--fingerprint' || fail "the refusal does not name --fingerprint: $out"
	[ "$(known_hosts_sum)" = "$before" ] || fail 'a host key was pinned without confirmation'
	[ -z "$(docker exec "$host" ls -A /opt/stacks)" ] || fail 'the stacks directory was filled without confirmation'

	step 'repo set refuses a wrong fingerprint'
	out=$(qd_fails repo set "$url" --fingerprint SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA) ||
		fail "repo set with a wrong fingerprint succeeded: $out"
	[ "$(known_hosts_sum)" = "$before" ] || fail 'a host key was pinned despite a wrong fingerprint'

	step 'repo set pins the host key and clones'
	expected=$(git_ed25519_fingerprint)
	out=$(qd --output json repo set "$url" --fingerprint "$expected") || fail "repo set failed: $out"
	printf '%s' "$out" | grep -q "$expected" || fail "repo set does not report the fingerprint $expected: $out"
	printf '%s' "$out" | grep -q '"cloned": *true' || fail "repo set did not clone: $out"
	docker exec "$host" test -f /opt/stacks/demo-stack/compose.yaml || fail 'the clone is missing in /opt/stacks'
	log "scenario: repo set pinned $expected and cloned"

	step 'repo fetch sees a commit pushed by the admin'
	out=$(qd --output json repo fetch) || fail "repo fetch failed: $out"
	printf '%s' "$out" | grep -q '"updates_available": *false' || fail "unexpected updates before the push: $out"
	as_admin 'cd ~/stacks &&
		sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.1.0/" demo-cache/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Update demo-cache to 1.1.0" &&
		git push -q origin main'
	pushed=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	out=$(qd --output json repo fetch) || fail "repo fetch failed: $out"
	printf '%s' "$out" | grep -q '"updates_available": *true' || fail "repo fetch does not see the new commit: $out"
	printf '%s' "$out" | grep -q "$pushed" || fail "repo fetch does not report the pushed head $pushed: $out"
	log "scenario: repo fetch saw $pushed"

	step 'stacks lists the demo stacks with image versions'
	out=$(qd --output json stacks) || fail "stacks failed: $out"
	log "scenario: stacks $out"
	for want in demo-stack demo-cache DEMO_APP_VERSION=1.0.1 DEMO_APP_VERSION=1.0.0; do
		printf '%s' "$out" | grep -q -- "$want" || fail "stacks does not show $want: $out"
	done

	step 'a changed host key of the git server aborts qdeploy'
	original_key=$(scanned_git_key)
	pinned=$(known_hosts_sum)
	docker exec "$prefix-git" sh -ec 'mkdir -p /root/hostkeys-orig && mv /etc/ssh/ssh_host_* /root/hostkeys-orig/ && ssh-keygen -A >/dev/null && kill -HUP 1'
	rotated=
	rotate_failure=
	i=0
	while [ "$(scanned_git_key)" = "$original_key" ]; do
		i=$((i + 1))
		if [ "$i" -ge 20 ]; then rotate_failure=1; break; fi
		sleep 1
	done
	fetch_rc=0 set_rc=0 fetch_out= set_out=
	if [ -z "$rotate_failure" ]; then
		new_fp=$(git_ed25519_fingerprint)
		fetch_out=$(qd repo fetch) || fetch_rc=$?
		set_out=$(qd repo set "$url" --fingerprint "$new_fp") || set_rc=$?
	fi
	after=$(known_hosts_sum)
	# Restore the original keys first, so a failed assertion below leaves the environment as it was.
	docker exec "$prefix-git" sh -ec 'rm -f /etc/ssh/ssh_host_* && mv /root/hostkeys-orig/ssh_host_* /etc/ssh/ && rmdir /root/hostkeys-orig && kill -HUP 1'
	i=0
	while [ "$(scanned_git_key)" != "$original_key" ]; do
		i=$((i + 1))
		[ "$i" -lt 20 ] || fail 'the original host keys of the git server are not served again'
		sleep 1
	done
	[ -z "$rotate_failure" ] || fail 'the git server did not serve the rotated host key'
	[ "$fetch_rc" -ne 0 ] || fail "repo fetch succeeded against a changed host key: $fetch_out"
	[ "$set_rc" -ne 0 ] || fail "repo set succeeded against a changed host key: $set_out"
	printf '%s' "$fetch_out" | grep -qi 'host key' || fail "repo fetch does not name the host key: $fetch_out"
	printf '%s' "$set_out" | grep -qi 'host key' || fail "repo set does not name the host key: $set_out"
	[ "$after" = "$pinned" ] || fail 'the pinned host key changed'
	log "scenario: changed host key refused: fetch: $fetch_out | set: $set_out"

	step 'the original host key works again'
	qd repo fetch >/dev/null || fail 'repo fetch fails after the host keys were restored'

	docker exec "$host" systemctl stop qdeploy-e2e
}

health_is() {
	[ "$(docker exec "$host" docker inspect -f '{{.State.Health.Status}}' "$1")" = "$2" ]
}
