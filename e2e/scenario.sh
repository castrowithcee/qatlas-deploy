#!/bin/sh
# Scenario: from the simulated host, clone the demo stacks repository over SSH and push a commit; then check
# the demo image tags and that a demo container turns unhealthy on purpose. Then install qdeploy on the host
# from a signed test release with the release installer, which sets up the service, and check its user, group
# and directories and that status needs root. Then bind the stacks repository with its own deploy key, list the
# stacks, deploy a new commit, show the available image versions per update ceiling without changing anything,
# roll back a commit with a failing healthcheck (data restored from the run's backup, revert commit on the
# remote), leave a stack whose rollback fails stopped and paused until resume, and refuse a changed host key of the git server.
# A second section takes over an installation of the earlier single binding as the first source and binds a second
# repository: its own stack is deployed, a stack named like one of the first source is reported and never deployed.
# Version updates with qdeploy stack update follow: qdeploy stack settings set commits and pushes the settings of a
# stack without a run, and they act on the next update: one direct step, a stepwise major jump and a jump whose
# middle step fails. The maintenance window follows on a real, short window: a stack in automatic mode is updated
# without a human, a stack in manual mode is only reported, a paused and a pinned stack stay as they are, and a change with
# window set takes effect in the running service. The terminal interface follows: sudo qdeploy opens it in a
# tmux pane, an update shows its changelog before it is confirmed and then runs, and two ticked stacks are updated
# one after another with one confirmation. Notification channels (webhook, Slack, Telegram, e-mail) are
# added with qdeploy notify: a webhook hears of a deployed and of a rolled-back run, and every channel
# type delivers a test message. Docker integration tests of the deployer run inside the host
# before all that. The last section resets the service to a fresh state and does the first setup only through
# the terminal interface: deploy key, registry credential, maintenance window, notification channel with its
# test, repository with a confirmed host key, a stack secret and the first deploy; then it restores an older
# backup through the interface and proves that no masked value shows anywhere. At the very end the installed
# program updates itself to a newer signed test release and refuses tampered ones. Sourced by run.sh scenario.

# signature_asset is the name of the release file with the signature; it must match release.SignatureAsset.
signature_asset=checksums.txt.sig

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

	scenario_compose_integration

	scenario_stacks_repo

	scenario_selfupdate

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

# release_build builds the fake release $1 (a tag) for the architecture of the host into $work/releases/$1 and
# signs its checksums with the test key. The binary trusts that key through a source overlay of the key list, so
# release.json and the sources stay as they are.
release_build() {
	tag=$1
	arch=$(docker exec "$host" dpkg --print-architecture)
	dir=$work/releasebuild
	if [ ! -f "$dir/overlay.json" ]; then
		mkdir -p "$dir"
		ssh-keygen -q -t ed25519 -N '' -C "$prefix-release-test-key" -f "$work/keys/release"
		printf 'qdeploy-release %s\n' "$(cut -d ' ' -f 1,2 "$work/keys/release.pub")" >"$dir/allowed-signers"
		sed "s|^var extraKeyLines string\$|var extraKeyLines = \"$(cut -d ' ' -f 1,2 "$work/keys/release.pub")\"|" \
			"$root/internal/release/release.go" >"$dir/release.go"
		grep -q '^var extraKeyLines = "ssh-ed25519 ' "$dir/release.go" || fail 'cannot add the test key to the release key list'
		printf '{"Replace":{"%s":"%s"}}' "$root/internal/release/release.go" "$dir/release.go" >"$dir/overlay.json"
		sed "s|\"targets\": .*|\"targets\": [\"linux/$arch\"],|" "$root/release.json" >"$dir/release.json"
	fi
	rel_signers=$(realpath --relative-to="$root" "$dir/allowed-signers")
	(cd "$root" && SOURCE_DATE_EPOCH=1700000000 GOFLAGS="${GOFLAGS:+$GOFLAGS }-overlay=$dir/overlay.json" \
		go run github.com/castrowithcee/qatlas-kit/cmd/releasebuild -config "$dir/release.json" -program qdeploy \
		-namespace qdeploy-release -signature-asset "$signature_asset" -allowed-signers "$rel_signers" \
		-releases-url https://github.com/castrowithcee/qatlas-deploy/releases -version "$tag" -out "$work/releases/$tag" >/dev/null) ||
		fail "cannot build the test release $tag"
	(cd "$work/releases/$tag" && ssh-keygen -q -Y sign -f "$work/keys/release" -n qdeploy-release checksums.txt) ||
		fail "cannot sign the test release $tag"
}

# release_publish makes the directory $work/releases/$1 (or the directory $2) the release $1 of the releases fake.
release_publish() {
	docker exec "$prefix-releases" rm -rf "/srv/releases/$1"
	docker cp "${2:-$work/releases/$1}" "$prefix-releases:/srv/releases/$1" || fail "cannot publish the test release $1"
}

# setup_service installs qdeploy from the fake release and starts the service. With the argument default-backup-image
# the service keeps the unit as installed and backs up with its default helper image; otherwise a drop-in names the
# local helper image, and the second release for the self-update section is built too.
setup_service() {
	step 'install qdeploy from a signed test release with the published installer'
	release_build v0.1.0
	[ "${1:-}" = default-backup-image ] || release_build v0.2.0
	release_publish v0.1.0
	as_admin 'curl -fsSL -o /tmp/install.sh https://github.com/castrowithcee/qatlas-deploy/releases/latest/download/install.sh &&
		sudo sh /tmp/install.sh && rm -f /tmp/install.sh' || fail 'the installer failed'
	# The backup helper image is a local one, so a drop-in (which an update keeps) names it.
	if [ "${1:-}" != default-backup-image ]; then
		docker exec "$host" sh -ec '
			install -d /etc/systemd/system/qdeploy.service.d
			printf "[Service]\nExecStart=\nExecStart=/usr/local/bin/qdeploy run --backup-image '"$backup_image"'\n" >/etc/systemd/system/qdeploy.service.d/e2e.conf
			systemctl daemon-reload' || fail 'cannot write the drop-in'
		docker exec "$host" systemctl restart qdeploy || fail 'cannot restart the qdeploy service'
	fi
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	log 'scenario: qdeploy service running'

	step 'the service runs as qdeploy without root, with the group docker, and keeps its directories closed'
	[ "$(docker exec "$host" systemctl is-active qdeploy)" = active ] || fail 'the qdeploy service is not active'
	pid=$(docker exec "$host" systemctl show -p MainPID --value qdeploy)
	[ -n "$pid" ] && [ "$pid" != 0 ] || fail 'the qdeploy service has no process'
	[ "$(docker exec "$host" stat -c %U "/proc/$pid")" = qdeploy ] || fail "the service does not run as qdeploy: $(docker exec "$host" stat -c %U "/proc/$pid")"
	if docker exec "$host" grep '^Uid:' "/proc/$pid/status" | grep -qw 0; then fail 'the service has a root user ID'; fi
	docker exec "$host" id -nG qdeploy | tr ' ' '\n' | grep -qx docker || fail 'the user qdeploy is not in the group docker'
	docker exec "$host" grep '^Groups:' "/proc/$pid/status" | grep -qw "$(docker exec "$host" getent group docker | cut -d : -f 3)" ||
		fail 'the service process does not hold the group docker'
	[ "$(docker exec "$host" stat -c %U:%G:%a /run/qdeploy)" = qdeploy:qdeploy:750 ] ||
		fail "/run/qdeploy is $(docker exec "$host" stat -c %U:%G:%a /run/qdeploy), want qdeploy:qdeploy:750"
	[ "$(docker exec "$host" stat -c %a /var/lib/qdeploy)" = 700 ] || fail "/var/lib/qdeploy has the mode $(docker exec "$host" stat -c %a /var/lib/qdeploy), want 700"
	if as_admin 'ls /run/qdeploy' >/dev/null 2>&1; then fail 'another user can list /run/qdeploy'; fi
	log "scenario: qdeploy runs as pid $pid, user qdeploy, group docker; /run/qdeploy qdeploy:qdeploy 750, /var/lib/qdeploy 700"

	step 'status needs root'
	out=$(as_admin 'sudo qdeploy status 2>&1') || fail "sudo qdeploy status failed: $out"
	printf '%s' "$out" | grep -qi 'running\|started\|uptime\|version' || fail "sudo qdeploy status does not show the service: $out"
	if out_plain=$(as_admin 'qdeploy status 2>&1'); then fail "qdeploy status without root succeeded: $out_plain"; fi
	log "scenario: sudo qdeploy status answers; without root: $out_plain"
}

# scenario_compose_integration runs the Docker integration tests of the deployer inside the host. The test
# binary is built here and refuses to run without the marker of the simulated host.
scenario_compose_integration() {
	step 'Docker integration tests of the deployer run on the host'
	(cd "$root" && GOOS=linux GOARCH="$(docker exec "$host" dpkg --print-architecture)" CGO_ENABLED=0 \
		go test -c -tags dockerintegration -o "$work/compose.test" ./internal/compose) || fail 'cannot build the integration tests'
	docker cp "$work/compose.test" "$host:/tmp/compose.test"
	out=$(docker exec -e QDEPLOY_E2E_HOST=1 -e "QDEPLOY_E2E_IMAGE=registry.$domain/demo/app:1.0.0" "$host" \
		/tmp/compose.test -test.v -test.count=1 -test.run '^TestDocker' 2>&1 </dev/null) || {
		printf '%s\n' "$out"
		docker exec "$host" rm -f /tmp/compose.test
		fail 'the Docker integration tests failed'
	}
	docker exec "$host" rm -f /tmp/compose.test
	for name in TestDockerHealthyStackIsDeployedAndChecked TestDockerUnhealthyStackFailsTheCheck TestDockerDeployWithoutSecretsFile; do
		printf '%s' "$out" | grep -q -- "--- PASS: $name" || fail "$name did not pass: $out"
	done
	if printf '%s' "$out" | grep -q -- '--- SKIP'; then fail "an integration test was skipped: $out"; fi
	log "scenario: Docker integration tests passed: $(printf '%s' "$out" | grep -E '^(--- |ok|PASS)' | tr '\n' ' ')"
}

# stack_field prints one flat field of a stack from qdeploy status (empty when it is absent).
stack_field() {
	qd --output json status | sed -n "s/.*\"stack\\.$1\\.$2\": *\"\\([^\"]*\\)\".*/\\1/p"
}

# stack_settled succeeds when the stack has no run in progress and its last run ended with the given result.
stack_settled() {
	[ -z "$(stack_field "$1" active_run)" ] && [ "$(stack_field "$1" last_run)" = "$2" ]
}

# stack_idle succeeds when the stack has no run in progress and has finished at least one.
stack_idle() {
	[ -z "$(stack_field "$1" active_run)" ] && [ -n "$(stack_field "$1" last_run)" ]
}

# run_finished succeeds when the log of the run has its result line and the stack has no run in progress.
run_finished() {
	[ -z "$(stack_field "$1" active_run)" ] && qd stack log "$2" | grep -q ' result '
}

has_failed_commit_finding() {
	stack_field demo-cache findings | grep -q failed-commit
}

# restart_service restarts the qdeploy unit, which makes the engine poll at once.
restart_service() {
	docker exec "$host" systemctl restart qdeploy || fail 'cannot restart the qdeploy service'
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	wait_for 'the qdeploy service to answer' 20 qd status
}

# status_field prints one top-level field of qdeploy status.
status_field() {
	qd --output json status | sed -n "s/.*\"$1\": *\"\([^\"]*\)\".*/\1/p"
}

# poll_done succeeds when the service has polled since it started.
poll_done() {
	started=$(status_field started_at) polled=$(status_field last_poll)
	[ -n "$started" ] && [ -n "$polled" ] && [ "$(printf '%s\n%s\n' "$polled" "$started" | sort | head -n 1)" = "$started" ]
}

# stack_healthy succeeds when the stack runs and every container with a healthcheck is healthy.
stack_healthy() {
	[ -n "$(docker exec "$host" docker ps -q --filter "label=com.docker.compose.project=$1" --filter health=healthy)" ] &&
		[ -z "$(docker exec "$host" docker ps -q --filter "label=com.docker.compose.project=$1" --filter health=unhealthy --filter health=starting)" ]
}

# log_steps prints the run's own steps of a run log in order, joined by commas; the sub-steps of the backup
# and the restore are left out.
log_steps() {
	printf '%s\n' "$1" | sed -n 's/^[^ ]* step //p' |
		grep -x -e stop -e backup -e pull -e up -e check -e rollback -e restore -e revert -e push -e fast-forward |
		tr '\n' ',' | sed 's/,$//'
}

rolled_back_steps=stop,backup,pull,up,check,rollback,stop,restore,revert,push,fast-forward,up,check
rollback_failed_steps=stop,backup,pull,up,check,rollback,stop,restore,revert,push,stop

# newest_run prints the id of the newest recorded run of a stack (empty when there is none).
newest_run() {
	qd stack runs "$1" | grep -m 1 -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}' || true
}

# next_run waits for a run of the stack other than the run id $2, active or already finished, and prints its
# id. Recorded runs are listed only once they have ended, so an active run is read from the status.
next_run() {
	i=0
	while :; do
		id=$(stack_field "$1" active_run)
		[ -n "$id" ] || id=$(newest_run "$1")
		if [ -n "$id" ] && [ "$id" != "$2" ]; then
			printf '%s' "$id"
			return 0
		fi
		i=$((i + 1))
		[ "$i" -lt 400 ] || return 1
		sleep 0.3
	done
}

# admin_commit changes a file of the admin clone with a sed expression and pushes it as a new commit.
admin_commit() {
	as_admin "cd ~/stacks && git pull -q --ff-only && sed -i '$2' $1 &&
		git -c user.name='E2E Admin' -c user.email=admin.qdeploye2e.test commit -q -am '$3' && git push -q origin main"
}

# stack_images prints the images of the running containers of a Compose project on the host.
stack_images() {
	docker exec "$host" docker ps --filter "label=com.docker.compose.project=$1" --format '{{.Image}}'
}

# qd_in runs qd with standard input from the first argument.
qd_in() {
	input=$1
	shift
	printf '%s' "$input" | docker exec -i "$host" qdeploy "$@" 2>&1
}

# hook_seen succeeds when the webhook receiver recorded a signed request to the path $1 with the event $2 and,
# if given, the stack $3 in its body.
hook_seen() {
	stack_pattern='"body"'
	[ -z "${3:-}" ] || stack_pattern="\\\"stack\\\":\\\"$3\\\""
	docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null | grep -F "\"path\":\"$1\"" |
		grep -F "\"X-Qdeploy-Event\":\"$2\"" | grep -F '"X-Qdeploy-Signature":"' | grep -qF -- "$stack_pattern"
}

# hook_has succeeds when the webhook receiver recorded a signed request to the path $1 for the event $2 whose
# body contains every further fixed string.
hook_has() {
	lines=$(docker exec "$prefix-hooks" cat /data/requests.jsonl 2>/dev/null | grep -F "\"path\":\"$1\"" |
		grep -F "\"X-Qdeploy-Event\":\"$2\"" | grep -F '"X-Qdeploy-Signature":"') || return 1
	shift 2
	for part in "$@"; do
		lines=$(printf '%s\n' "$lines" | grep -F -- "$part") || return 1
	done
}

# journal prints the log of the qdeploy service.
journal() {
	docker exec "$host" journalctl -u qdeploy --no-pager -o cat
}

# mail_seen succeeds when the mail server recorded a message containing the fixed string $1.
mail_seen() {
	docker exec "$prefix-mail" cat /data/messages.jsonl 2>/dev/null | grep -F -- "$1" >/dev/null
}

hook_url=https://hooks.$domain/notify
hook_signing_value=demo-signing-value-e2e
slack_url_value=https://hooks.$domain/slack/T000/B000/demo-slack-path-e2e
bot_credential=123456:demo-bot-credential-e2e
mail_login_value=demo-mail-login-e2e

scenario_deploy() {
	secret=not-a-real-secret-e2e

	step 'the runs started by repo set end; secrets.env is not there yet'
	wait_for 'the first run of demo-stack' 120 stack_idle demo-stack
	wait_for 'the first run of demo-cache' 120 stack_idle demo-cache
	log "scenario: first runs: demo-stack $(stack_field demo-stack last_run), demo-cache $(stack_field demo-cache last_run)"

	step 'demo-plain (docker-compose.yml, name: demo-shop, no secrets.env) is deployed as project demo-shop'
	wait_for 'the first run of demo-shop' 120 stack_idle demo-shop
	[ "$(stack_images demo-shop)" = "registry.$domain/demo/app:1.0.0" ] || fail "demo-shop does not run the image 1.0.0: $(stack_images demo-shop)"
	[ -z "$(stack_images demo-plain)" ] || fail "a project named after the folder demo-plain runs: $(stack_images demo-plain)"
	qd --output json stack list | grep -q -- demo-shop || fail 'stack list does not name demo-shop'
	if docker exec "$host" test -e /opt/stacks/demo-plain/secrets.env; then fail 'demo-plain has a secrets.env'; fi

	step 'secrets.env is created on the host, owned by the service user'
	for stack in demo-stack demo-cache; do
		docker exec -u qdeploy "$host" sh -ec "umask 077 && printf 'DEMO_SECRET=%s\n' '$secret' >/opt/stacks/$stack/secrets.env"
	done
	dirty=$(docker exec -u qdeploy "$host" git -C /opt/stacks status --porcelain 2>&1) || fail "git status failed: $dirty"
	[ -z "$dirty" ] || fail "secrets.env made the working copy dirty: $dirty"
	[ "$(docker exec "$host" stat -c %U:%a /opt/stacks/demo-stack/secrets.env)" = qdeploy:600 ] || fail 'secrets.env has the wrong owner or mode'

	step 'a webhook channel is added from standard input'
	out=$(qd_in "$hook_url
$hook_signing_value
" notify add hook --type webhook --events succeeded,rolled-back) || fail "notify add failed: $out"
	qd notify list | grep -q 'hook *webhook *succeeded,rolled-back' || fail "notify list does not show the webhook: $(qd notify list)"

	step 'a manual run deploys demo-stack and records the commit'
	out=$(qd --output json stack deploy demo-stack) || fail "deploy failed: $out"
	run=$(printf '%s\n' "$out" | sed -n 's/.*"run": *"\([^"]*\)".*/\1/p')
	commit=$(printf '%s\n' "$out" | sed -n 's/.*"commit": *"\([^"]*\)".*/\1/p')
	[ -n "$run" ] && [ -n "$commit" ] || fail "deploy did not report a run and a commit: $out"
	wait_for "the run $run" 240 run_finished demo-stack "$run"
	log_text=$(qd stack log "$run")
	printf '%s' "$log_text" | grep -q ' result succeeded' || fail "the run did not succeed: $log_text"
	[ "$(stack_field demo-stack deployed)" = "$commit" ] || fail "demo-stack is not recorded as deployed at $commit: $(qd --output json status)"
	[ "$(stack_images demo-stack)" = "registry.$domain/demo/app:1.0.1" ] || fail "demo-stack does not run the image 1.0.1: $(stack_images demo-stack)"
	docker exec "$host" docker inspect -f '{{.Config.Env}}' "$(docker exec "$host" docker ps -q --filter label=com.docker.compose.project=demo-stack)" |
		grep -q "DEMO_SECRET=$secret" || fail 'the container did not get the value of secrets.env'
	log "scenario: deploy succeeded, run $run, deployed commit $commit"
	wait_for 'the webhook for the deployed run' 60 hook_seen /notify succeeded demo-stack
	log "scenario: the webhook receiver was told of the succeeded run"

	step 'the engine deploys a new commit it finds by polling'
	good=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	wait_for 'the run of demo-cache' 240 stack_settled demo-cache succeeded
	[ "$(stack_field demo-cache deployed)" = "$good" ] || fail "demo-cache is not recorded as deployed at $good: $(qd --output json status)"
	[ "$(stack_images demo-cache)" = "registry.$domain/demo/app:1.1.0" ] || fail "demo-cache does not run the image 1.1.0: $(stack_images demo-cache)"
	log "scenario: poll deployed demo-cache at $good"

	# Here every stack runs its newest commit, so the poll at the restart of the repair has nothing to run.
	step 'a bound installation without the grant gets it at the next root client command'
	docker exec "$host" sh -ec 'rm -f /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf && systemctl daemon-reload' ||
		fail 'cannot remove the stack folder grant'
	if docker exec "$host" systemctl show -p ReadWritePaths qdeploy | grep -qF /opt/stacks; then fail 'the grant is still active after its removal'; fi
	pid=$(service_pid)
	out=$(qd status) || fail "qdeploy status failed without the grant: $out"
	printf '%s' "$out" | grep -qF 'has no write grant' || fail "no notice about the missing grant: $out"
	docker exec "$host" grep -qx 'ReadWritePaths=-/opt/stacks' /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf ||
		fail 'the grant was not written again'
	docker exec "$host" systemctl show -p ReadWritePaths qdeploy | grep -qF /opt/stacks || fail 'the service has no write access after the repair'
	wait_for 'the service after the repair' 60 service_serving
	[ "$(service_pid)" != "$pid" ] || fail 'the repair did not restart the service'
	out=$(qd status) || fail "qdeploy status failed: $out"
	if printf '%s' "$out" | grep -qF 'has no write grant'; then fail "the repair repeated: $out"; fi

	step 'a commit with a failing healthcheck is rolled back with a revert commit on the remote branch'
	as_admin 'cd ~/stacks &&
		sed -i "s/^DEMO_HEALTH=.*/DEMO_HEALTH=fail/" demo-cache/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Break the demo-cache healthcheck" &&
		git push -q origin main'
	bad=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	wait_for 'the rolled-back run of demo-cache' 240 stack_settled demo-cache rolled-back
	runs_out=$(qd stack runs demo-cache)
	failed_run=$(printf '%s\n' "$runs_out" | grep -m 1 -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}')
	printf '%s\n' "$runs_out" | grep -m 1 "$failed_run" | grep -q 'rolled-back' || fail "the newest run of demo-cache is not rolled-back: $runs_out"
	failed_log=$(qd stack log "$failed_run")
	printf '%s' "$failed_log" | grep -q ' result rolled-back' || fail "the run log does not end rolled-back: $failed_log"
	printf '%s' "$failed_log" | grep -q 'unhealthy' || fail "the run log does not name the unhealthy container: $failed_log"
	[ "$(log_steps "$failed_log")" = "$rolled_back_steps" ] || fail "unexpected steps of the rolled-back run: $(log_steps "$failed_log")"
	printf '%s' "$failed_log" | grep -q 'skipped: the stack has no backup rules' || fail "the rollback did not skip the data restore for a stack without backup rules: $failed_log"
	revert=$(as_admin "cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD")
	[ "$revert" != "$bad" ] || fail 'no revert commit reached the remote branch'
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$revert" ] || fail 'the revert commit is not the head of the remote branch'
	meta=$(as_admin "cd ~/stacks && git log -1 --format='%an|%ae|%cn|%ce|%s|%P' $revert")
	[ "$meta" = "qdeploy|qdeploy@localhost.invalid|qdeploy|qdeploy@localhost.invalid|Revert demo-cache to $(printf '%s' "$good" | cut -c 1-12)|$bad" ] ||
		fail "the revert commit has the wrong author, subject or parent: $meta"
	[ "$(as_admin "cd ~/stacks && git rev-parse $revert:demo-cache")" = "$(as_admin "cd ~/stacks && git rev-parse $good:demo-cache")" ] ||
		fail 'the revert commit does not restore the demo-cache folder'
	[ "$(stack_field demo-cache deployed)" = "$revert" ] || fail "demo-cache is recorded as deployed at $(stack_field demo-cache deployed), want the revert commit $revert"
	[ "$(stack_images demo-cache)" = "registry.$domain/demo/app:1.1.0" ] || fail "demo-cache does not run the previous image 1.1.0: $(stack_images demo-cache)"
	stack_healthy demo-cache || fail 'demo-cache is not healthy after the rollback'
	log "scenario: failing healthcheck: run $failed_run rolled back, bad commit $bad reverted by $revert"
	wait_for 'the webhook for the rolled-back run' 60 hook_seen /notify rolled-back demo-cache
	log "scenario: the webhook receiver was told of the rolled-back run"

	step 'a rolled-back commit is not run again by the next poll'
	count=$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	restart_service
	wait_for 'the poll after the restart' 60 poll_done
	[ "$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$count" ] || fail 'a rolled-back commit was run again by the poll'
	[ "$(stack_field demo-cache deployed)" = "$revert" ] || fail 'the poll changed the deployed commit of demo-cache'
	[ -z "$(stack_field demo-cache pending)" ] || fail "demo-cache has a pending commit after the rollback: $(stack_field demo-cache pending)"
	[ -z "$(stack_field demo-cache findings)" ] || fail "demo-cache has findings after the rollback: $(stack_field demo-cache findings)"

	step 'no run log holds a secret value'
	ids=$(qd stack runs | grep -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}')
	n=0
	for id in $ids; do
		n=$((n + 1))
		if qd stack log "$id" | grep -qF -- "$secret"; then fail "the log of run $id holds the secret value"; fi
	done
	if qd stack log "$(newest_run demo-shop)" | grep -qF -- 'not-a-real-secret'; then fail 'the run log of demo-shop holds its DEMO_PASSWORD value'; fi
	[ "$n" -ge 3 ] || fail "too few runs to check the logs: $n"
	if qd stack runs | grep -qF -- "$secret"; then fail 'qdeploy stack runs shows the secret value'; fi
	log "scenario: the secret value is in none of $n run logs"

	scenario_notify
}

# scenario_notify proves that every channel type delivers a test message, that a channel is managed with the
# documented errors, and that no secret part of a channel shows in the listing, the run logs or the journal.
scenario_notify() {
	step 'notify test delivers through the webhook channel'
	out=$(qd notify test hook) || fail "notify test hook failed: $out"
	hook_seen /notify test || fail 'the webhook receiver did not record the test message'

	step 'notify test delivers through a Slack channel'
	out=$(qd_in "$slack_url_value
" notify add chat --type slack) || fail "notify add chat failed: $out"
	out=$(qd notify test chat) || fail "notify test chat failed: $out"
	docker exec "$prefix-hooks" cat /data/requests.jsonl | grep -F '"path":"/slack/T000/B000/demo-slack-path-e2e"' | grep -qF 'qdeploy test message' ||
		fail 'the receiver did not record the Slack test message'

	step 'notify test delivers through a Telegram channel'
	out=$(qd_in "$bot_credential
" notify add bot --type telegram --chat-id 4242 --base-url "https://hooks.$domain/telegram") || fail "notify add bot failed: $out"
	out=$(qd notify test bot) || fail "notify test bot failed: $out"
	docker exec "$prefix-hooks" cat /data/requests.jsonl | grep -F "\"path\":\"/telegram/bot$bot_credential/sendMessage\"" | grep -qF '4242' ||
		fail 'the receiver did not record the Telegram test message'

	step 'notify test delivers through an SMTP channel with STARTTLS and a login'
	out=$(qd_in "$mail_login_value
" notify add mail --type smtp --host "mail.$domain" --user qdeploy-mailer --from qdeploy@example.test \
		--to ops@example.test) || fail "notify add mail failed: $out"
	out=$(qd notify test mail) || fail "notify test mail failed: $out"
	mail_seen 'Subject: qdeploy test message' || fail 'the mail server did not record the test message'
	mail_seen '"to":["TO:\u003cops@example.test\u003e"]' || fail 'the mail server did not record the recipient'
	mail_seen '"user":"qdeploy-mailer"' || fail 'the mail server did not record the login name'

	step 'the channel commands report their errors'
	if out=$(qd_in "$slack_url_value
" notify add hook --type slack); then fail 'a duplicate channel was added'; fi
	printf '%s' "$out" | grep -q 'exists' || fail "the duplicate was not explained: $out"
	out=$(qd_fails notify test missing) || fail 'a missing channel was tested'
	printf '%s' "$out" | grep -q 'no such channel' || fail "the missing channel was not explained: $out"
	out=$(qd_fails notify remove missing) || fail 'a missing channel was removed'
	if out=$(qd_in "$slack_url_value
" notify add bad --type slack --events exploded); then fail 'an unknown event was accepted'; fi
	printf '%s' "$out" | grep -q 'event' || fail "the unknown event was not explained: $out"
	if qd notify list | grep -q '^bad '; then fail 'a refused channel was stored'; fi

	step 'no secret part of a channel shows in the listing, the run logs or the journal'
	listing=$(qd notify list; qd --output json notify list)
	runs=$(qd stack runs)
	for id in $(qd stack runs | grep -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}'); do
		runs="$runs
$(qd stack log "$id")"
	done
	journal_text=$(journal)
	for value in "$hook_url" "$hook_signing_value" "$slack_url_value" demo-slack-path-e2e "$bot_credential" "$mail_login_value"; do
		for place in listing runs journal_text; do
			eval "text=\$$place"
			if printf '%s' "$text" | grep -qF -- "$value"; then fail "a secret part of a channel shows in $place"; fi
		done
	done
	[ -n "$journal_text" ] || fail 'the journal of the service is empty'

	step 'removing the channels removes their stored secrets'
	for name in hook chat bot mail; do
		out=$(qd notify remove "$name") || fail "notify remove $name failed: $out"
	done
	if qd notify list | grep -q 'webhook\|slack\|telegram\|smtp'; then fail "channels remain: $(qd notify list)"; fi
	left=$(docker exec "$host" sh -c 'ls /var/lib/qdeploy/credentials 2>/dev/null | grep -c "^notify" || true')
	[ "$left" = 0 ] || fail "$left notification credentials remain after the channels were removed"
	log 'scenario: all four channel types delivered a test message and no secret part showed anywhere'
}

# fixture_run runs a script as root in a throwaway container of the backup helper image on the host, with the
# data source (a host directory or a volume name) mounted at /data.
fixture_run() {
	docker exec -i "$host" docker run --rm --network none -v "$1:/data" "$backup_image" sh -ec "$2"
}

# fixture_write fills /data with entries of mixed numeric owners and modes.
fixture_write() {
	fixture_run "$1" '
		cd /data
		mkdir -p sub/deep
		printf "alpha\n" >a.txt && chown 1234:1234 a.txt && chmod 640 a.txt
		printf "root only\n" >r.txt && chown 0:0 r.txt && chmod 600 r.txt
		printf "nested\n" >sub/deep/n.bin && chown 4321:5678 sub/deep/n.bin && chmod 755 sub/deep/n.bin
		chown 2000:2000 sub && chmod 750 sub
		ln -s a.txt link'
}

# fixture_change_script alters, removes and adds entries in the current directory so the data differs from
# fixture_write in content, owners and modes.
fixture_change_script() {
	printf '%s' '
		rm -f a.txt
		printf "changed\n" >r.txt && chown 99:99 r.txt && chmod 666 r.txt
		chown 0:0 sub && chmod 777 sub
		rm -rf sub/deep
		printf "extra\n" >extra.txt'
}

fixture_change() {
	fixture_run "$1" "cd /data; $(fixture_change_script)"
}

# fixture_snap prints numeric owner, group, mode, type and link target of every entry, then the checksums.
fixture_snap() {
	docker exec -i "$host" docker run --rm --network none -v "$1:/data:ro" "$backup_image" sh -ec '
		cd /data
		find . -printf "%U:%G:%m:%y %p -> %l\n" | sort -k2
		find . -type f -exec sha256sum {} + | sort -k2'
}

backup_ids() {
	qd stack backup list "$1" | grep -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}'
}

stack_running() {
	[ "$(stack_images "$1")" = "registry.$domain/demo/app:1.0.1" ]
}

scenario_backup() {
	bind=/opt/stacks/demo-stack/data
	vol=demo-stack_state
	store=/var/lib/qdeploy/backups/demo-stack

	step 'demo-stack data and volume get files with mixed owners and modes'
	wait_for 'demo-stack to be idle' 120 stack_idle demo-stack
	stack_running demo-stack || fail "demo-stack is not running before the backup: $(stack_images demo-stack)"
	fixture_write "$bind" || fail 'cannot write the fixture into data/'
	fixture_write "$vol" || fail 'cannot write the fixture into the volume'
	bind_before=$(fixture_snap "$bind") && vol_before=$(fixture_snap "$vol") || fail 'cannot read the fixture'
	printf '%s\n' "$bind_before" | grep -q '^1234:1234:640:f ./a.txt' || fail "the fixture lacks the owner 1234: $bind_before"
	printf '%s\n' "$bind_before" | grep -q '^0:0:600:f ./r.txt' || fail "the fixture lacks the root file: $bind_before"
	log "scenario: fixture written: $(printf '%s\n' "$bind_before" | grep -c ' ->') entries in data/ and in the volume"

	step 'backup create saves data/, the volume and the command output, and starts the stack again'
	out=$(qd --output json stack backup create demo-stack) || fail "backup create failed: $out"
	b1=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	[ -n "$b1" ] || fail "backup create reported no id: $out"
	printf '%s' "$out" | grep -q '"items": *3' || fail "the backup does not hold three items: $out"
	stack_running demo-stack || fail "demo-stack is not running after the backup: $(stack_images demo-stack)"
	[ "$(docker exec "$host" cat "$store/$b1/command.out")" = backup-command-output ] || fail "command.out of $b1 is wrong: $(docker exec "$host" ls -l "$store/$b1")"
	[ "$(docker exec "$host" stat -c %a:%U "$store/$b1/path-0.tar" "$store/$b1/volume-0.tar" | sort -u)" = 600:qdeploy ] ||
		fail "the archives are not private to the service user: $(docker exec "$host" ls -ln "$store/$b1")"
	log "scenario: backup $b1: $(docker exec "$host" sh -c "cd $store/$b1 && ls | tr '\n' ' '")"

	step 'the data changes after the backup'
	fixture_change "$bind" && fixture_change "$vol" || fail 'cannot change the data'
	bind_changed=$(fixture_snap "$bind") && vol_changed=$(fixture_snap "$vol") || fail 'cannot read the changed data'
	[ "$bind_changed" != "$bind_before" ] && [ "$vol_changed" != "$vol_before" ] || fail 'the change did not alter the data'

	step 'restore without confirmation and without a terminal is refused and changes nothing'
	out=$(qd_fails stack backup restore demo-stack "$b1") || fail "backup restore without confirmation succeeded: $out"
	printf '%s' "$out" | grep -q -- '--yes' || fail "the refusal does not name --yes: $out"
	[ "$(fixture_snap "$bind")" = "$bind_changed" ] && [ "$(fixture_snap "$vol")" = "$vol_changed" ] || fail 'the refused restore changed the data'
	stack_running demo-stack || fail 'the refused restore stopped the stack'
	log "scenario: restore refused: $out"

	step 'restore with --yes brings back data/ and the volume byte-identical, owners and modes included'
	out=$(qd stack backup restore demo-stack "$b1" --yes) || fail "backup restore failed: $out"
	[ "$(fixture_snap "$bind")" = "$bind_before" ] || fail "data/ differs after the restore: $(fixture_snap "$bind")"
	[ "$(fixture_snap "$vol")" = "$vol_before" ] || fail "the volume differs after the restore: $(fixture_snap "$vol")"
	stack_running demo-stack || fail "demo-stack is not running after the restore: $(stack_images demo-stack)"
	log "scenario: restored $b1; data/ and volume identical to the backed-up state"

	step 'retention keeps the newest two backups and deletes the oldest'
	out=$(qd --output json stack backup create demo-stack) || fail "second backup failed: $out"
	b2=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	out=$(qd --output json stack backup create demo-stack) || fail "third backup failed: $out"
	b3=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	printf '%s' "$out" | grep -q "\"removed\": *\"$b1\"" || fail "the third backup did not report removing $b1: $out"
	listed=$(backup_ids demo-stack | tr '\n' ' ')
	[ "$listed" = "$b3 $b2 " ] || fail "backup list shows '$listed', want '$b3 $b2 '"
	if docker exec "$host" test -e "$store/$b1"; then fail "the oldest backup $b1 is still in the store"; fi
	[ "$(docker exec "$host" ls "$store" | wc -l)" = 2 ] || fail "the store does not hold exactly two backups: $(docker exec "$host" ls "$store")"
	stack_running demo-stack || fail 'demo-stack is not running after the backups'
	log "scenario: retention: list shows $listed, $b1 deleted"

	step 'a backup path outside the stack directory is refused'
	out=$(qd_fails stack check demo-badpath) || fail "check accepted a path outside the stack: $out"
	printf '%s' "$out" | grep -q -- "'..' is not allowed" || fail "check does not name the path error: $out"
	log "scenario: check refused: $out"

	step 'stack check reports the functions of a stack with settings and of one without'
	out=$(qd --output json stack check demo-stack) || fail "stack check of demo-stack failed: $out"
	case $out in *"$secret"*) fail 'stack check shows a secret value' ;; esac
	printf '%s' "$out" | jq -e '.errors == 0 and (.stacks | length) == 1 and
		(.stacks[0].functions | map(.function)) == ["deploy", "update", "rollback", "backup", "redact"] and
		all(.stacks[0].functions[]; .works) and .stacks[0].functions[1].mode == "manual"' >/dev/null ||
		fail "unexpected check of demo-stack: $out"
	out=$(qd --output json stack check demo-shop) || fail "stack check of demo-shop failed: $out"
	printf '%s' "$out" | jq -e '.errors == 0 and
		([.stacks[0].functions[] | {(.function): .works}] | add) == {deploy: true, update: false, rollback: false, backup: false, redact: true} and
		.stacks[0].functions[1].mode == "none" and (.stacks[0].functions[1].action | length) > 0' >/dev/null ||
		fail "unexpected check of demo-shop: $out"
	log "scenario: check of demo-stack and demo-shop as expected"
	out=$(qd_fails stack backup create demo-badpath) || fail "backup create accepted a path outside the stack: $out"
	printf '%s' "$out" | grep -q -- 'config-invalid' || fail "backup create does not refuse the invalid settings: $out"
	if docker exec "$host" test -e /var/lib/qdeploy/backups/demo-badpath; then fail 'a refused backup left a store directory'; fi
	log "scenario: backup create refused: $out"
}

# json_value prints the first string value of a key in compact JSON (empty when the key is absent or empty).
json_value() {
	printf '%s' "$1" | grep -o "\"$2\":\"[^\"]*\"" | head -n 1 | cut -d '"' -f 4
}

# updates_expect checks the single row of a 'qdeploy stack updates' JSON output: current, available, major_available
# (empty for none) and status.
updates_expect() {
	out=$1
	[ "$(json_value "$out" current)" = "$2" ] && [ "$(json_value "$out" available)" = "$3" ] &&
		[ "$(json_value "$out" major_available)" = "$4" ] && [ "$(json_value "$out" status)" = "$5" ] ||
		fail "unexpected updates, want current=$2 available=$3 major_available=$4 status=$5: $out"
	case $out in *2.1.0-rc.1*) fail "a pre-release is offered: $out" ;; esac
}

# demo_stack_settings prints the settings of demo-stack: the backup section the repository starts with and,
# if given, the update section $1, whose backslash escapes printf %b expands.
demo_stack_settings() {
	printf 'version: 1\n%bbackup:\n  paths:\n    - data\n  volumes:\n    - state\n  command: [/bin/echo, backup-command-output]\n  retention:\n    keep: 2\n' "${1:-}"
}

# settings_set replaces the settings of the stack $1 with the YAML $2 from standard input and keeps the JSON
# answer in settings_out. The service commits and pushes them and moves its working copy; no run starts.
settings_set() {
	settings_out=$(qd_in "$2" --output json stack settings set "$1" --file -) || fail "settings set of $1 failed: $settings_out"
}

# scenario_updates shows the update detection against the registry: the stored result, the ceiling of the stack,
# the major hint and a stack without settings. Detection changes nothing; only commits do.
scenario_updates() {
	step 'updates says a stack was never checked'
	out=$(qd --output json stack updates demo-stack) || fail "updates failed: $out"
	[ "$(json_value "$out" status)" = never-checked ] || fail "demo-stack is not reported as never checked: $out"

	step 'updates --refresh asks the registry; the default ceiling is minor and a major version is only a hint'
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates --refresh failed: $out"
	updates_expect "$out" 1.0.1 1.1.0 5.0.0 update
	[ -n "$(json_value "$out" checked)" ] || fail "the result has no time of the check: $out"
	stored=$(qd --output json stack updates demo-stack) || fail "updates failed: $stored"
	[ "$stored" = "$out" ] || fail "updates without --refresh does not show the stored result: $stored"
	log "scenario: updates $out"

	step 'a stack without settings is listed too'
	out=$(qd --output json stack updates demo-cache --refresh) || fail "updates demo-cache failed: $out"
	updates_expect "$out" 1.1.0 1.1.0 5.0.0 current

	step 'ceiling patch offers no minor step; ceiling major offers 5.0.0 without a hint'
	settings_set demo-stack "$(demo_stack_settings 'update:\n  ceiling: patch\n')"
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates failed: $out"
	updates_expect "$out" 1.0.1 1.0.1 5.0.0 current
	settings_set demo-stack "$(demo_stack_settings 'update:\n  ceiling: major\n')"
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates failed: $out"
	updates_expect "$out" 1.0.1 5.0.0 '' update

	step 'the demo-stack settings are restored'
	settings_set demo-stack "$(demo_stack_settings)"
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates failed: $out"
	updates_expect "$out" 1.0.1 1.1.0 5.0.0 update
	[ "$(stack_images demo-stack)" = "registry.$domain/demo/app:1.0.1" ] || fail "updates changed the running image: $(stack_images demo-stack)"
	[ "$(as_admin 'cd ~/stacks && git pull -q --ff-only && git log --format=%s -n 1 -- demo-stack/.env')" = 'Update demo-stack to 1.0.1' ] ||
		fail 'the version of demo-stack was changed'
}

# scenario_rollback runs the automatic rollback against the demo stacks: a good deploy with a backup, a bad
# deploy of a stack with data (data restored from the run's own backup, revert commit pushed, previous state
# healthy), and a bad deploy whose rollback fails (stack stopped and paused, deploy refused, resume).
scenario_rollback() {
	bind=/opt/stacks/demo-stack/data
	vol=demo-stack_state
	image=registry.$domain/demo/app

	step 'a good commit deploys demo-stack with a backup of its data'
	prev=$(newest_run demo-stack)
	admin_commit demo-stack/.env 's/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.1.0/' 'Update demo-stack to 1.1.0'
	good_stack=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	run=$(next_run demo-stack "$prev") || fail 'no run of demo-stack started for the good commit'
	wait_for "the run $run" 240 run_finished demo-stack "$run"
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result succeeded' || fail "the good run did not succeed: $run_log"
	[ "$(log_steps "$run_log")" = stop,backup,pull,up,check ] || fail "unexpected steps of the good run: $(log_steps "$run_log")"
	good_backup=$(printf '%s\n' "$run_log" | sed -n 's/^[^ ]* info backup \([0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}\)$/\1/p')
	[ -n "$good_backup" ] || fail "the good run made no backup: $run_log"
	[ "$(backup_ids demo-stack | head -n 1)" = "$good_backup" ] || fail "backup list does not show $good_backup first: $(qd stack backup list demo-stack)"
	[ "$(stack_field demo-stack deployed)" = "$good_stack" ] || fail "demo-stack is not recorded as deployed at $good_stack"
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "demo-stack does not run 1.1.0: $(stack_images demo-stack)"
	log "scenario: good run $run deployed $good_stack, backup $good_backup"

	step 'a failing commit restores the data of the run backup, reverts the commit and runs the previous state'
	marker=marker-$run
	for target in "$bind" "$vol"; do
		fixture_run "$target" "printf '%s\n' $marker >/data/marker.txt && chown 4242:4242 /data/marker.txt && chmod 604 /data/marker.txt" ||
			fail 'cannot write the marker'
	done
	bind_before=$(fixture_snap "$bind") && vol_before=$(fixture_snap "$vol") || fail 'cannot read the data before the run'
	printf '%s\n' "$bind_before" | grep -q '^4242:4242:604:f ./marker.txt' || fail "the marker is missing: $bind_before"
	old_backups=$(backup_ids demo-stack | tr '\n' ' ')
	prev=$run
	as_admin 'cd ~/stacks && git pull -q --ff-only &&
		sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=2.0.0/; s/^DEMO_HEALTH=.*/DEMO_HEALTH=fail/" demo-stack/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Update demo-stack to 2.0.0 with a broken healthcheck" &&
		git push -q origin main'
	bad_stack=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	run=$(next_run demo-stack "$prev") || fail 'no run of demo-stack started for the failing commit'
	# Once the run has its backup, the data changes under the new version; only a restore can undo that.
	i=0
	until qd stack log "$run" | grep -q ' info backup [0-9]\{8\}T'; do
		i=$((i + 1))
		[ "$i" -lt 120 ] || fail "the run $run made no backup"
		sleep 0.2
	done
	docker exec -i "$host" docker run --rm --network none -v "$bind:/data" -v "$vol:/state" "$backup_image" sh -ec \
		"for d in /data /state; do cd \$d; $(fixture_change_script); done" </dev/null || fail 'cannot change the data during the run'
	wait_for "the run $run" 240 run_finished demo-stack "$run"
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result rolled-back' || fail "the run did not end rolled-back: $run_log"
	[ "$(log_steps "$run_log")" = "$rolled_back_steps" ] || fail "unexpected steps of the rolled-back run: $(log_steps "$run_log")"
	run_backups=$(printf '%s\n' "$run_log" | sed -n 's/^[^ ]* info backup \([0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}\)$/\1/p')
	[ "$(printf '%s\n' "$run_backups" | wc -l)" = 2 ] && [ "$(printf '%s\n' "$run_backups" | sort -u | wc -l)" = 1 ] ||
		fail "the backup and the restore do not name one backup: $run_backups"
	run_backup=$(printf '%s\n' "$run_backups" | head -n 1)
	case " $old_backups " in *" $run_backup "*) fail "the run restored an older backup: $run_backup" ;; esac
	backup_ids demo-stack | grep -qx "$run_backup" || fail "backup list does not know $run_backup: $(qd stack backup list demo-stack)"
	[ "$(fixture_snap "$bind")" = "$bind_before" ] || fail "data/ differs from the backed-up state after the rollback: $(fixture_snap "$bind")"
	[ "$(fixture_snap "$vol")" = "$vol_before" ] || fail "the volume differs from the backed-up state after the rollback: $(fixture_snap "$vol")"
	[ "$(docker exec "$host" cat "$bind/marker.txt")" = "$marker" ] || fail 'the marker of this run backup is not back'
	revert=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	[ "$revert" != "$bad_stack" ] || fail 'no revert commit reached the remote branch'
	meta=$(as_admin "cd ~/stacks && git log -1 --format='%an|%ae|%cn|%ce|%s|%P' $revert")
	[ "$meta" = "qdeploy|qdeploy@localhost.invalid|qdeploy|qdeploy@localhost.invalid|Revert demo-stack to $(printf '%s' "$good_stack" | cut -c 1-12)|$bad_stack" ] ||
		fail "the revert commit has the wrong author, subject or parent: $meta"
	[ "$(as_admin "cd ~/stacks && git rev-parse $revert:demo-stack")" = "$(as_admin "cd ~/stacks && git rev-parse $good_stack:demo-stack")" ] ||
		fail 'the revert commit does not restore the demo-stack folder'
	[ "$(stack_field demo-stack deployed)" = "$revert" ] || fail "demo-stack is not recorded as deployed at the revert commit $revert"
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "demo-stack does not run the previous image 1.1.0: $(stack_images demo-stack)"
	stack_healthy demo-stack || fail 'demo-stack is not healthy after the rollback'
	log "scenario: run $run rolled back; restored $run_backup (not among $old_backups); revert $revert; data identical to the backed-up state"

	step 'a failing rollback leaves the stack stopped and paused'
	cache_good=$(stack_field demo-cache deployed)
	prev=$(newest_run demo-cache)
	admin_commit demo-cache/.env 's/^DEMO_HEALTH=.*/DEMO_HEALTH=fail/' 'Break the demo-cache healthcheck again'
	stuck=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	# The remote refuses every push from now on, so the revert commit cannot be published.
	docker exec "$prefix-git" sh -ec "printf '#!/bin/sh\necho rejected by the e2e hook >&2\nexit 1\n' >/srv/git/stacks.git/hooks/pre-receive &&
		chown git:git /srv/git/stacks.git/hooks/pre-receive && chmod 755 /srv/git/stacks.git/hooks/pre-receive"
	restart_service
	run=$(next_run demo-cache "$prev") || {
		docker exec "$prefix-git" rm -f /srv/git/stacks.git/hooks/pre-receive
		fail 'no run of demo-cache started for the failing commit'
	}
	wait_for "the run $run" 240 run_finished demo-cache "$run" || {
		docker exec "$prefix-git" rm -f /srv/git/stacks.git/hooks/pre-receive
		fail "the run $run did not finish"
	}
	docker exec "$prefix-git" rm -f /srv/git/stacks.git/hooks/pre-receive
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result rollback-failed' || fail "the run did not end rollback-failed: $run_log"
	[ "$(log_steps "$run_log")" = "$rollback_failed_steps" ] || fail "unexpected steps of the rollback-failed run: $(log_steps "$run_log")"
	printf '%s' "$run_log" | grep -q 'rejected by the e2e hook' || fail "the run log does not show the remote refusing the push: $run_log"
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$stuck" ] || fail 'the remote branch moved although the push was refused'
	[ -z "$(stack_images demo-cache)" ] || fail "demo-cache is still running after the failed rollback: $(stack_images demo-cache)"
	[ -n "$(stack_field demo-cache paused)" ] || fail "status does not show demo-cache as paused: $(qd --output json status)"
	[ "$(stack_field demo-cache deployed)" = "$cache_good" ] || fail 'the failed rollback changed the deployed commit'
	log "scenario: run $run rollback-failed; demo-cache stopped and $(stack_field demo-cache paused)"

	step 'deploy is refused while the stack is paused and the poll does not run it'
	out=$(qd_fails stack deploy demo-cache) || fail "deploy of a paused stack succeeded: $out"
	printf '%s' "$out" | grep -q 'paused' || fail "the refusal does not name the pause: $out"
	count=$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	restart_service
	wait_for 'the poll after the restart' 60 poll_done
	[ "$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$count" ] || fail 'the poll ran a paused stack'
	[ -n "$(stack_field demo-cache paused)" ] || fail 'the restart cleared the pause'
	stack_field demo-cache findings | grep -q failed-commit || fail "the failed commit is not marked as a finding: $(stack_field demo-cache findings)"
	log "scenario: deploy refused: $out"

	step 'resume clears the pause, and the stack deploys a fixing commit'
	out=$(qd --output json stack resume demo-cache) || fail "resume failed: $out"
	printf '%s' "$out" | grep -q '"stack": *"demo-cache"' || fail "resume does not name the stack: $out"
	[ -z "$(stack_field demo-cache paused)" ] || fail 'status still shows the pause after resume'
	if stack_field demo-cache findings | grep -q paused; then fail 'the paused finding stays after resume'; fi
	out=$(qd_fails stack resume demo-cache) || fail "a second resume succeeded: $out"
	prev=$(newest_run demo-cache)
	admin_commit demo-cache/.env 's/^DEMO_HEALTH=.*/DEMO_HEALTH=ok/; s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=2.0.0/' 'Fix the demo-cache healthcheck and update to 2.0.0'
	fix=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	run=$(next_run demo-cache "$prev") || fail 'no run of demo-cache started for the fixing commit'
	wait_for "the run $run" 240 run_finished demo-cache "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run of the fixing commit did not succeed: $(qd stack log "$run")"
	[ "$(stack_field demo-cache deployed)" = "$fix" ] || fail "demo-cache is not recorded as deployed at $fix"
	[ "$(stack_images demo-cache)" = "$image:2.0.0" ] || fail "demo-cache does not run 2.0.0: $(stack_images demo-cache)"
	stack_healthy demo-cache || fail 'demo-cache is not healthy after the fix'
	log "scenario: resumed; fixing commit $fix deployed"
}

# json_values prints every string value of a key in JSON output, one per line.
json_values() {
	printf '%s' "$1" | grep -o "\"$2\": *\"[^\"]*\"" | cut -d '"' -f 4
}

# update_reset sets the major strategy $2 of demo-stack, commits the version $1 and waits for its run.
update_reset() {
	settings_set demo-stack "$(demo_stack_settings "update:\\n  major: $2\\n")"
	prev=$(newest_run demo-stack)
	admin_commit demo-stack/.env "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=$1/" "Reset demo-stack to $1"
	restart_service
	run=$(next_run demo-stack "$prev") || fail "no run of demo-stack started for the reset to $1"
	wait_for "the run $run" 240 run_finished demo-stack "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run for the reset to $1 did not succeed: $(qd stack log "$run")"
	[ "$(stack_images demo-stack)" = "registry.$domain/demo/app:$1" ] || fail "demo-stack does not run $1: $(stack_images demo-stack)"
}

# update_commit_only_version requires that a commit of the stacks repo changes one line of demo-stack/.env, the
# image variable, from one version to another.
update_commit_only_version() {
	files=$(as_admin "cd ~/stacks && git show --format= --numstat $1")
	[ "$files" = "$(printf '1\t1\tdemo-stack/.env')" ] || fail "the commit $1 touches more than one line of demo-stack/.env: $files"
	diff_lines=$(as_admin "cd ~/stacks && git show --format= -U0 $1 | grep '^[-+]' | grep -v '^\(---\|+++\)'")
	[ "$diff_lines" = "$(printf -- '-DEMO_APP_VERSION=%s\n+DEMO_APP_VERSION=%s' "$2" "$3")" ] ||
		fail "the commit $1 does not only move DEMO_APP_VERSION from $2 to $3: $diff_lines"
	[ "$(as_admin "cd ~/stacks && git log -1 --format=%s $1")" = "Update demo-stack DEMO_APP_VERSION from $2 to $3" ] ||
		fail "the commit $1 has an unexpected subject: $(as_admin "cd ~/stacks && git log -1 --format=%s $1")"
}

# update_run_backup prints the backup a run log names.
update_run_backup() {
	printf '%s\n' "$1" | sed -n 's/^[^ ]* info backup \([0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}\)$/\1/p' | head -n 1
}

# update_steps prints the steps of an update run in order, joined by commas.
update_steps() {
	printf '%s\n' "$1" | sed -n 's/^[^ ]* step //p' |
		grep -x -e stop -e backup -e commit -e push -e pull -e up -e check -e rollback -e restore -e revert -e fast-forward |
		tr '\n' ',' | sed 's/,$//'
}

# scenario_update takes version steps with 'qdeploy stack update' against the real git server and registry: a direct
# update, a stepwise major jump with a commit and a backup per step, and a stepwise jump whose middle step has a
# failing healthcheck. The image 4.0.0 is unhealthy by itself, so no commit but the update's own can break it.
scenario_update() {
	image=registry.$domain/demo/app

	step 'a stack without settings is not updated'
	out=$(qd_fails stack update demo-cache) || fail "update accepted a stack without settings: $out"
	printf '%s' "$out" | grep -q 'has no settings' || fail "the refusal does not say that the stack has no settings: $out"

	step 'stack settings set commits and pushes the settings of demo-stack without a run; equal settings commit nothing'
	direct=$(demo_stack_settings 'update:\n  major: direct\n')
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	deployed_before=$(stack_field demo-stack deployed)
	runs_before=$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	settings_set demo-stack "$direct"
	printf '%s' "$settings_out" | grep -q '"changed": *true' || fail "settings set reports no change: $settings_out"
	commit=$(printf '%s\n' "$settings_out" | sed -n 's/.*"commit": *"\([0-9a-f]*\)".*/\1/p')
	[ -n "$commit" ] || fail "settings set reports no commit: $settings_out"
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$commit" ] || fail "the settings commit $commit is not the head of the remote branch"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	meta=$(as_admin "cd ~/stacks && git log -1 --format='%s|%P' $commit")
	[ "$meta" = "qdeploy: settings of demo-stack|$head_before" ] || fail "the settings commit has the wrong subject or parent: $meta"
	files=$(as_admin "cd ~/stacks && git show --format= --name-only $commit")
	[ "$files" = .qatlas/qdeploy/demo-stack.yaml ] || fail "the settings commit touches more than the settings file: $files"
	as_admin 'cd ~/stacks && grep -qx "  major: direct" .qatlas/qdeploy/demo-stack.yaml' ||
		fail "the settings file does not hold major direct: $(as_admin 'cat ~/stacks/.qatlas/qdeploy/demo-stack.yaml')"
	[ "$(docker exec -u qdeploy "$host" git -C /opt/stacks rev-parse HEAD)" = "$commit" ] || fail 'the working copy on the host did not follow the settings commit'
	dirty=$(docker exec -u qdeploy "$host" git -C /opt/stacks status --porcelain 2>&1) || fail "git status failed: $dirty"
	[ -z "$dirty" ] || fail "the settings left the working copy on the host dirty: $dirty"
	out=$(qd --output json stack settings demo-stack) || fail "stack settings failed: $out"
	printf '%s' "$out" | grep -q '"major": *"direct"' || fail "stack settings does not show major direct: $out"
	settings_set demo-stack "$direct"
	printf '%s' "$settings_out" | grep -q '"changed": *false' || fail "equal settings were committed again: $settings_out"
	if printf '%s' "$settings_out" | grep -q '"commit"'; then fail "equal settings report a commit: $settings_out"; fi
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$commit" ] || fail 'equal settings moved the remote branch'
	[ "$(stack_field demo-stack deployed)" = "$deployed_before" ] || fail 'the settings commit changed the deployed commit of demo-stack'
	[ "$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$runs_before" ] || fail 'the settings started a run of demo-stack'
	log "scenario: settings commit $commit pushed; the working copy on the host is clean; a second set changed nothing"

	step 'with major direct, an update creates one commit that moves only the image variable, and the stack runs the new version'
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "demo-stack does not run 1.1.0 before the update: $(stack_images demo-stack)"
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	runs_before=$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	out=$(qd --output json stack update demo-stack --version 3.0.0) || fail "update to 3.0.0 failed: $out"
	[ "$(json_values "$out" to | tr '\n' ' ')" = '3.0.0 ' ] || fail "a direct update is not one step to 3.0.0: $out"
	run=$(json_values "$out" run | head -n 1)
	commit=$(json_values "$out" commit | head -n 1)
	[ -n "$run" ] && [ -n "$commit" ] || fail "update reported no run and commit: $out"
	run_log=$(qd stack log "$run")
	log "scenario: direct update steps: $(update_steps "$run_log")"
	printf '%s' "$run_log" | grep -q ' result succeeded' || fail "the update run did not succeed: $run_log"
	[ "$(update_steps "$run_log")" = stop,backup,commit,push,fast-forward,pull,up,check ] || fail "unexpected steps of the update run: $(update_steps "$run_log")"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 1 ] || fail 'the update did not create exactly one commit'
	[ "$(as_admin 'cd ~/stacks && git rev-parse HEAD')" = "$commit" ] || fail "the head of the branch is not the update commit $commit"
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$commit" ] || fail 'the update commit is not the head of the remote branch'
	update_commit_only_version "$commit" 1.1.0 3.0.0
	[ "$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$((runs_before + 1))" ] || fail 'the update did not make exactly one run'
	[ "$(stack_field demo-stack deployed)" = "$commit" ] || fail "demo-stack is not recorded as deployed at $commit"
	[ "$(stack_images demo-stack)" = "$image:3.0.0" ] || fail "demo-stack does not run 3.0.0: $(stack_images demo-stack)"
	stack_healthy demo-stack || fail 'demo-stack is not healthy after the update'
	[ -n "$(update_run_backup "$run_log")" ] || fail "the update run made no backup: $run_log"
	log "scenario: direct update: run $run, commit $commit, demo-stack runs 3.0.0"

	step 'a stepwise major jump takes one step per major version, each with its own commit and backup'
	update_reset 1.1.0 stepwise
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	runs_before=$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	out=$(qd --output json stack update demo-stack --version 3.0.0) || fail "stepwise update to 3.0.0 failed: $out"
	[ "$(json_values "$out" to | tr '\n' ' ')" = '2.0.0 3.0.0 ' ] || fail "the jump to 3.0.0 is not the steps 2.0.0 and 3.0.0: $out"
	[ "$(json_values "$out" result | sort -u)" = succeeded ] || fail "not every step succeeded: $out"
	runs=$(json_values "$out" run)
	commits=$(json_values "$out" commit)
	[ "$(printf '%s\n' "$runs" | sort -u | wc -l)" = 2 ] && [ "$(printf '%s\n' "$commits" | sort -u | wc -l)" = 2 ] ||
		fail "the steps do not have their own run and commit: $out"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 2 ] || fail 'the stepwise update did not create exactly two commits'
	[ "$(as_admin "cd ~/stacks && git log --reverse --format=%H $head_before..HEAD")" = "$commits" ] || fail "the commits on the branch are not the steps' commits: $commits"
	first_commit=$(printf '%s\n' "$commits" | sed -n 1p) second_commit=$(printf '%s\n' "$commits" | sed -n 2p)
	update_commit_only_version "$first_commit" 1.1.0 2.0.0
	update_commit_only_version "$second_commit" 2.0.0 3.0.0
	backups=
	for id in $runs; do
		run_log=$(qd stack log "$id")
		printf '%s' "$run_log" | grep -q ' result succeeded' || fail "the step run $id did not succeed: $run_log"
		[ "$(update_steps "$run_log")" = stop,backup,commit,push,fast-forward,pull,up,check ] || fail "unexpected steps of the run $id: $(update_steps "$run_log")"
		this_backup=$(update_run_backup "$run_log")
		[ -n "$this_backup" ] || fail "the step run $id made no backup"
		case " $backups " in *" $this_backup "*) fail "the step runs share the backup $this_backup" ;; esac
		backup_ids demo-stack | grep -qx "$this_backup" || fail "backup list does not know $this_backup"
		backups="$backups $this_backup"
	done
	[ "$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$((runs_before + 2))" ] || fail 'the stepwise update did not make exactly two runs'
	[ "$(stack_images demo-stack)" = "$image:3.0.0" ] || fail "demo-stack does not run 3.0.0: $(stack_images demo-stack)"
	[ "$(stack_field demo-stack deployed)" = "$second_commit" ] || fail "demo-stack is not recorded as deployed at $second_commit"
	log "scenario: stepwise update: commits $(printf '%s' "$commits" | tr '\n' ' '), backups$backups"

	step 'a failing middle step ends with a revert commit, the previous step stays, later steps are not attempted'
	update_reset 2.0.0 stepwise
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	deployed_before=$(stack_field demo-stack deployed)
	runs_before=$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	out=$(qd_fails --output json stack update demo-stack --version 5.0.0) || fail "update across a failing step succeeded: $out"
	log "scenario: failing jump: $out"
	[ "$(json_values "$out" to | tr '\n' ' ')" = '3.0.0 4.0.0 5.0.0 ' ] || fail "the jump to 5.0.0 is not the steps 3.0.0, 4.0.0 and 5.0.0: $out"
	[ "$(json_values "$out" result | tr '\n' ' ')" = 'succeeded rolled-back skipped ' ] || fail "unexpected results of the steps: $out"
	runs=$(json_values "$out" run)
	[ "$(printf '%s\n' "$runs" | wc -l)" = 2 ] || fail "only the first two steps should have a run: $out"
	[ "$(qd stack runs demo-stack | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$((runs_before + 2))" ] || fail 'the later step was attempted or a step made no run'
	good_run=$(printf '%s\n' "$runs" | sed -n 1p) bad_run=$(printf '%s\n' "$runs" | sed -n 2p)
	good_log=$(qd stack log "$good_run") bad_log=$(qd stack log "$bad_run")
	printf '%s' "$good_log" | grep -q ' result succeeded' || fail "the run of the 3.0.0 step did not succeed: $good_log"
	printf '%s' "$bad_log" | grep -q ' result rolled-back' || fail "the run of the 4.0.0 step did not end rolled-back: $bad_log"
	printf '%s' "$bad_log" | grep -q 'unhealthy' || fail "the failed run does not name the unhealthy container: $bad_log"
	log "scenario: failed step steps: $(update_steps "$bad_log")"
	[ "$(update_steps "$bad_log")" = stop,backup,commit,push,fast-forward,pull,up,check,rollback,stop,restore,revert,push,fast-forward,up,check ] ||
		fail "unexpected steps of the failed step: $(update_steps "$bad_log")"
	[ -n "$(update_run_backup "$bad_log")" ] || fail "the failed step made no backup: $bad_log"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 3 ] || fail 'the failing jump did not leave exactly the step, the failed step and its revert on the branch'
	step_commit=$(as_admin "cd ~/stacks && git log --reverse --format=%H $head_before..HEAD" | sed -n 1p)
	bad_step=$(as_admin "cd ~/stacks && git log --reverse --format=%H $head_before..HEAD" | sed -n 2p)
	revert=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	update_commit_only_version "$step_commit" 2.0.0 3.0.0
	update_commit_only_version "$bad_step" 3.0.0 4.0.0
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$revert" ] || fail 'the revert commit is not the head of the remote branch'
	meta=$(as_admin "cd ~/stacks && git log -1 --format='%an|%ae|%cn|%ce|%s|%P' $revert")
	[ "$meta" = "qdeploy|qdeploy@localhost.invalid|qdeploy|qdeploy@localhost.invalid|Revert demo-stack to $(printf '%s' "$step_commit" | cut -c 1-12)|$bad_step" ] ||
		fail "the revert commit has the wrong author, subject or parent: $meta"
	[ "$(as_admin "cd ~/stacks && git rev-parse $revert:demo-stack")" = "$(as_admin "cd ~/stacks && git rev-parse $step_commit:demo-stack")" ] ||
		fail 'the revert commit does not restore the demo-stack folder of the previous step'
	if [ -n "$(as_admin "cd ~/stacks && git log --format=%s --grep='to 5.0.0'")" ]; then fail 'a commit for 5.0.0 reached the branch'; fi
	[ "$(as_admin "cd ~/stacks && git show $revert:demo-stack/.env | grep '^DEMO_APP_VERSION='")" = DEMO_APP_VERSION=3.0.0 ] || fail 'the branch does not hold 3.0.0'
	[ "$(stack_field demo-stack deployed)" = "$revert" ] || fail "demo-stack is not recorded as deployed at the revert commit $revert"
	[ "$(stack_images demo-stack)" = "$image:3.0.0" ] || fail "demo-stack does not run the previous step 3.0.0: $(stack_images demo-stack)"
	stack_healthy demo-stack || fail 'demo-stack is not healthy after the rollback'
	[ -z "$deployed_before" ] || [ "$deployed_before" != "$revert" ] || fail 'no revert was made'
	log "scenario: failing jump: run $bad_run rolled back, step commit $step_commit kept, revert $revert, 5.0.0 not attempted"

	step 'the demo-stack settings and version are restored'
	update_reset 1.1.0 stepwise
	settings_set demo-stack "$(demo_stack_settings)"
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "demo-stack does not run 1.1.0 again: $(stack_images demo-stack)"
}

scenario_stacks_repo() {
	setup_service
	url=git.$domain:stacks.git

	# The default window (daily 03:00) must not open in the middle of the other sections on a real clock.
	window_far_away >/dev/null || fail 'cannot move the maintenance window out of the way'

	step 'deploy key'
	out=$(as_admin 'sudo qdeploy --output json key generate 2>&1') || fail "key generate failed: $out"
	pub=$(printf '%s\n' "$out" | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	case $pub in ssh-ed25519\ *) ;; *) fail "no public key in: $out" ;; esac
	shown=$(as_admin 'sudo qdeploy --output json key show 2>&1' | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	[ "$shown" = "$pub" ] || fail "key show differs from key generate: $shown"
	fingerprint=$(printf '%s\n' "$pub" | docker exec -i "$host" sh -c 'cat >/tmp/deploy-key.pub && ssh-keygen -lf /tmp/deploy-key.pub; rm -f /tmp/deploy-key.pub') ||
		fail "ssh-keygen cannot parse the deploy key: $fingerprint"
	case $fingerprint in *SHA256:*ED25519*) ;; *) fail "unexpected fingerprint: $fingerprint" ;; esac
	log "scenario: deploy key shown and parsed: $fingerprint"
	printf '%s\n' "$pub" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	log 'scenario: deploy key registered on the git server'

	step 'repo set refuses an unconfirmed host key without a terminal'
	before=$(known_hosts_sum)
	out=$(qd_fails repo set "$url" --dir /opt/stacks --create) || fail "repo set without terminal and fingerprint succeeded: $out"
	printf '%s' "$out" | grep -q -- '--fingerprint' || fail "the refusal does not name --fingerprint: $out"
	[ "$(known_hosts_sum)" = "$before" ] || fail 'a host key was pinned without confirmation'
	if docker exec "$host" test -e /opt/stacks; then fail 'the stack folder was created although the host key was not confirmed'; fi

	step 'repo set without --dir and without --create creates no stack folder'
	out=$(qd_fails repo set "$url" --fingerprint "$(git_ed25519_fingerprint)") || fail "repo set without --dir succeeded: $out"
	printf '%s' "$out" | grep -q -- '--dir' || fail "the refusal does not name --dir: $out"
	out=$(qd_fails repo set "$url" --dir /opt/stacks --fingerprint "$(git_ed25519_fingerprint)") ||
		fail "repo set for a missing folder without --create succeeded: $out"
	printf '%s' "$out" | grep -q -- '--create' || fail "the refusal does not name --create: $out"
	if docker exec "$host" test -e /opt/stacks; then fail 'the stack folder was created without confirmation'; fi
	if docker exec "$host" test -e /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf; then fail 'a refused repo set wrote the drop-in'; fi

	step 'repo set refuses a wrong fingerprint'
	out=$(qd_fails repo set "$url" --dir /opt/stacks --create --fingerprint SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA) ||
		fail "repo set with a wrong fingerprint succeeded: $out"
	[ "$(known_hosts_sum)" = "$before" ] || fail 'a host key was pinned despite a wrong fingerprint'
	if docker exec "$host" test -e /opt/stacks; then fail 'the stack folder was created despite a wrong fingerprint'; fi

	step 'repo set pins the host key and clones'
	expected=$(git_ed25519_fingerprint)
	out=$(qd --output json repo set "$url" --dir /opt/stacks --create --fingerprint "$expected") || fail "repo set failed: $out"
	printf '%s' "$out" | grep -q "$expected" || fail "repo set does not report the fingerprint $expected: $out"
	printf '%s' "$out" | grep -q '"cloned": *true' || fail "repo set did not clone: $out"
	docker exec "$host" test -f /opt/stacks/demo-stack/compose.yaml || fail 'the clone is missing in /opt/stacks'
	[ "$(docker exec "$host" stat -c %U:%G:%a /opt/stacks)" = qdeploy:qdeploy:755 ] ||
		fail "/opt/stacks is $(docker exec "$host" stat -c %U:%G:%a /opt/stacks), want qdeploy:qdeploy:755"
	docker exec "$host" grep -qx 'ReadWritePaths=-/opt/stacks' /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf ||
		fail 'the drop-in does not grant /opt/stacks'
	docker exec "$host" systemctl show -p ReadWritePaths qdeploy | grep -qF /opt/stacks || fail 'the service has no write access to /opt/stacks'
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

	step 'stack list shows the demo stacks with image versions'
	out=$(qd --output json stack list) || fail "stack list failed: $out"
	log "scenario: stack list $out"
	for want in demo-stack demo-cache DEMO_APP_VERSION=1.0.1 DEMO_APP_VERSION=1.0.0; do
		printf '%s' "$out" | grep -q -- "$want" || fail "stack list does not show $want: $out"
	done

	scenario_deploy

	scenario_containers

	scenario_container_logs

	scenario_container_actions

	scenario_stack_actions

	scenario_repair_start

	scenario_container_monitor

	scenario_backup

	scenario_updates

	scenario_rollback

	scenario_update

	scenario_window

	scenario_tui

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
		set_out=$(qd repo set "$url" --dir /opt/stacks --fingerprint "$new_fp") || set_rc=$?
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

	scenario_sources

	scenario_root_source

	scenario_repair_end

	scenario_tui_setup
}

# flat_field prints one flat field of qdeploy status by its full name (empty when it is absent).
flat_field() {
	qd --output json status | sed -n "s/.*\"$1\": *\"\\([^\"]*\\)\".*/\\1/p"
}

collision_seen() {
	flat_field source.opt-stacks2.stack.demo-stack.findings | grep -q name-collision
}

# scenario_sources proves the service with several stack folders. An installation bound to one repository by an
# earlier version is represented by the state layout that version left (stacks-repo.json, the engine state in
# deploy/), made from the current one while the service is stopped; the start must take it over with state and
# deploy key. Then a second repository becomes a second source: its own stack is deployed, a stack named like one of
# the first source is reported with name-collision and never deployed.
scenario_sources() {
	state=/var/lib/qdeploy
	url2=git.$domain:stacks2.git

	step 'an installation of the earlier single binding is taken over with its state and deploy key'
	key_before=$(qd --output json key show | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	deployed_before=$(stack_field demo-stack deployed)
	run_before=$(stack_field demo-stack last_run)
	images_before=$(stack_images demo-stack)
	[ -n "$key_before" ] && [ -n "$deployed_before" ] && [ -n "$run_before" ] || fail 'the first source has no key, deployment or run to carry over'
	docker exec "$host" systemctl stop qdeploy || fail 'cannot stop the qdeploy service'
	url1=$(docker exec "$host" sed -n 's/.*"url": *"\([^"]*\)".*/\1/p' "$state/sources.json")
	[ "$url1" = "$url" ] || fail "sources.json does not bind $url: $url1"
	docker exec "$host" test -d "$state/sources/opt-stacks/deploy" || fail 'the engine state of the first source is not below sources/opt-stacks/deploy'
	docker exec "$host" sh -ec "
		mv $state/sources/opt-stacks/deploy $state/deploy
		rmdir $state/sources/opt-stacks $state/sources
		rm $state/sources.json
		printf '{\"version\":1,\"url\":\"$url1\",\"dir\":\"/opt/stacks\"}' >$state/stacks-repo.json
		chown qdeploy:qdeploy $state/stacks-repo.json && chmod 600 $state/stacks-repo.json" || fail 'cannot lay out the earlier state'
	docker exec "$host" systemctl start qdeploy || fail 'cannot start the qdeploy service on the earlier state'
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	wait_for 'the qdeploy service to answer' 20 qd status
	wait_for 'the poll of the taken-over source' 60 poll_done
	if docker exec "$host" test -e "$state/stacks-repo.json"; then fail 'the old binding file is still the binding'; fi
	if docker exec "$host" test -e "$state/deploy"; then fail 'the engine state was not moved'; fi
	docker exec "$host" test -d "$state/sources/opt-stacks/deploy" || fail 'the engine state is not below sources/opt-stacks'
	docker exec "$host" grep -q '"key": *"deploy-key"' "$state/sources.json" || fail "the first source does not keep the deploy key: $(docker exec "$host" cat "$state/sources.json")"
	[ "$(qd --output json key show | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')" = "$key_before" ] || fail 'the deploy key changed with the takeover'
	[ "$(flat_field source.opt-stacks.path)" = /opt/stacks ] || fail "status does not show the first source: $(qd --output json status)"
	[ "$(stack_field demo-stack source)" = opt-stacks ] || fail "demo-stack is not listed with its source: $(qd --output json status)"
	[ "$(stack_field demo-stack deployed)" = "$deployed_before" ] || fail 'the deployed commit of demo-stack was lost with the takeover'
	[ "$(stack_field demo-stack last_run)" = "$run_before" ] || fail 'the takeover started a run of demo-stack'
	qd repo fetch >/dev/null || fail 'repo fetch fails with the taken-over key'
	log 'scenario: the single binding became the source opt-stacks with its state and key'

	step 'a second repository is bound as a second source with its own deploy key'
	out=$(qd --output json key generate --dir /opt/stacks2) || fail "key generate --dir failed: $out"
	pub2=$(printf '%s\n' "$out" | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	case $pub2 in ssh-ed25519\ *) ;; *) fail "no public key in: $out" ;; esac
	[ "$pub2" != "$key_before" ] || fail 'the second source got the key of the first'
	[ "$(qd --output json key show --dir /opt/stacks2 | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')" = "$pub2" ] || fail 'key show --dir differs from key generate --dir'
	[ "$(qd --output json key show | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')" = "$key_before" ] || fail 'a key for another folder replaced the key of the first source'
	printf '%s\n' "$pub2" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	out=$(qd --output json repo set "$url2" --dir /opt/stacks2 --create --fingerprint "$(git_ed25519_fingerprint)") || fail "repo set for the second source failed: $out"
	printf '%s' "$out" | grep -q '"cloned": *true' || fail "repo set did not clone the second source: $out"
	docker exec "$host" test -f /opt/stacks2/demo-second/compose.yaml || fail 'the clone is missing in /opt/stacks2'
	docker exec "$host" grep -qx 'ReadWritePaths=-/opt/stacks2' /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf || fail 'the drop-in does not grant /opt/stacks2'
	wait_for 'the qdeploy service to answer' 30 qd status
	[ "$(flat_field source.opt-stacks2.path)" = /opt/stacks2 ] || fail "status does not show the second source: $(qd --output json status)"

	step 'the own stack of the second source is deployed'
	wait_for 'the first run of demo-second' 180 stack_settled demo-second succeeded
	[ "$(stack_images demo-second)" = "registry.$domain/demo/app:1.0.0" ] || fail "demo-second does not run the image 1.0.0: $(stack_images demo-second)"
	[ "$(stack_field demo-second source)" = opt-stacks2 ] || fail "demo-second is not listed with its source: $(qd --output json status)"
	out=$(qd --output json stack list) || fail "stack list failed: $out"
	printf '%s' "$out" | grep -q 'opt-stacks2' || fail "stack list does not show the source of demo-second: $out"

	step 'a stack named like one of the first source is reported with name-collision and never deployed'
	wait_for 'the finding name-collision' 60 collision_seen
	[ "$(flat_field source.opt-stacks2.stack.demo-stack.source)" = opt-stacks2 ] || fail "the blocked entry has no source: $(qd --output json status)"
	[ -z "$(flat_field source.opt-stacks2.stack.demo-stack.deployed)" ] || fail 'the blocked stack was deployed'
	[ -z "$(flat_field source.opt-stacks2.stack.demo-stack.last_run)" ] || fail 'the blocked stack has a run'
	[ "$(stack_field demo-stack source)" = opt-stacks ] || fail 'the stack of the first source lost its name'
	[ "$(stack_field demo-stack deployed)" = "$deployed_before" ] || fail 'the stack of the first source changed'
	[ "$(stack_images demo-stack)" = "$images_before" ] || fail "demo-stack changed its image: $(stack_images demo-stack) instead of $images_before"
	if docker exec "$host" docker ps -a --format '{{.Names}}' --filter label=com.docker.compose.project=demo-stack | grep -q . &&
		docker exec "$host" docker ps -a --filter label=com.docker.compose.project=demo-stack --format '{{.Image}}' | grep -q ':2.0.0$'; then
		fail 'a container of the duplicate demo-stack runs'
	fi
	log 'scenario: second source deployed demo-second; its demo-stack is blocked by name-collision'
}

# source_stack_count prints how many stacks status lists for the source $1.
source_stack_count() {
	qd --output json status | grep -c "\"stack\\.[^.]*\\.source\": *\"$1\"" || true
}

# root_commit changes a file of the admin clone of the third repository and pushes it as a new commit.
root_commit() {
	as_admin "cd ~/stacks3 && git pull -q --ff-only && sed -i '$2' $1 &&
		git -c user.name='E2E Admin' -c user.email=admin.qdeploye2e.test commit -q -am '$3' && git push -q origin main"
}

# scenario_root_source proves a stack folder whose root holds the compose file: the whole folder is exactly one
# stack, a subfolder with a compose file of its own is only reported, a version change by commit is deployed and
# a failing one is rolled back with a revert commit of the whole folder.
scenario_root_source() {
	url3=git.$domain:stacks3.git
	image=registry.$domain/demo/app

	step 'a third repository with its compose file in the root is bound as one more source'
	out=$(qd --output json key generate --dir /opt/stacks3) || fail "key generate --dir failed: $out"
	pub3=$(printf '%s\n' "$out" | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p')
	case $pub3 in ssh-ed25519\ *) ;; *) fail "no public key in: $out" ;; esac
	printf '%s\n' "$pub3" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	out=$(qd --output json repo set "$url3" --dir /opt/stacks3 --create --fingerprint "$(git_ed25519_fingerprint)") || fail "repo set for the third source failed: $out"
	printf '%s' "$out" | grep -q '"cloned": *true' || fail "repo set did not clone the third source: $out"
	docker exec "$host" test -f /opt/stacks3/compose.yaml || fail 'the clone in /opt/stacks3 has no compose file in its root'

	step 'the root of the source is deployed as exactly one stack; its subfolder is no stack'
	wait_for 'the first run of demo-root' 180 stack_settled demo-root succeeded
	[ "$(source_stack_count opt-stacks3)" = 1 ] || fail "the third source is not exactly one stack: $(qd --output json status)"
	[ "$(stack_field demo-root source)" = opt-stacks3 ] || fail "demo-root is not listed with its source: $(qd --output json status)"
	[ -z "$(stack_field demo-extra source)" ] || fail 'the subfolder with a compose file is a stack'
	[ "$(stack_images demo-root)" = "$image:1.0.0" ] || fail "demo-root does not run the image 1.0.0: $(stack_images demo-root)"
	[ "$(docker exec "$host" docker ps --filter label=com.docker.compose.project=demo-root --format '{{.Label "com.docker.compose.project.working_dir"}}')" = /opt/stacks3 ] ||
		fail 'demo-root does not run from the root of the source'
	[ -z "$(stack_images demo-extra)" ] || fail 'a container of the subfolder runs'
	out=$(qd --output json stack check demo-root) || fail "stack check of demo-root failed: $out"
	printf '%s' "$out" | jq -e '.errors == 0 and ([.stacks[].findings[] | select(.rule == "ignored-stack-folder")] | length) == 1' >/dev/null ||
		fail "stack check does not report ignored-stack-folder once: $out"
	as_admin "rm -rf ~/stacks3 && git clone -q $url3 ~/stacks3" || fail 'cannot clone the third repository as admin'

	step 'a version change by commit is deployed'
	prev=$(newest_run demo-root)
	root_commit .env 's/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.1.0/' 'Update demo-root to 1.1.0'
	good_root=$(as_admin 'cd ~/stacks3 && git rev-parse HEAD')
	restart_service
	run=$(next_run demo-root "$prev") || fail 'no run of demo-root started for the version change'
	wait_for "the run $run" 240 run_finished demo-root "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run of the version change did not succeed: $(qd stack log "$run")"
	[ "$(stack_field demo-root deployed)" = "$good_root" ] || fail "demo-root is not recorded as deployed at $good_root"
	[ "$(stack_images demo-root)" = "$image:1.1.0" ] || fail "demo-root does not run 1.1.0: $(stack_images demo-root)"

	step 'a failing version is rolled back and the revert commit restores the whole folder'
	prev=$run
	root_commit .env 's/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=2.0.0/; s/^DEMO_HEALTH=.*/DEMO_HEALTH=fail/' 'Update demo-root to 2.0.0 with a broken healthcheck'
	bad_root=$(as_admin 'cd ~/stacks3 && git rev-parse HEAD')
	restart_service
	run=$(next_run demo-root "$prev") || fail 'no run of demo-root started for the failing commit'
	wait_for "the run $run" 240 run_finished demo-root "$run"
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result rolled-back' || fail "the run did not end rolled-back: $run_log"
	revert=$(as_admin 'cd ~/stacks3 && git pull -q --ff-only && git rev-parse HEAD')
	[ "$revert" != "$bad_root" ] || fail 'no revert commit reached the remote branch'
	meta=$(as_admin "cd ~/stacks3 && git log -1 --format='%an|%ae|%s|%P' $revert")
	[ "$meta" = "qdeploy|qdeploy@localhost.invalid|Revert the stack folder to $(printf '%s' "$good_root" | cut -c 1-12)|$bad_root" ] ||
		fail "the revert commit has the wrong author, subject or parent: $meta"
	[ "$(as_admin "cd ~/stacks3 && git rev-parse $revert^{tree}")" = "$(as_admin "cd ~/stacks3 && git rev-parse $good_root^{tree}")" ] ||
		fail 'the revert commit does not restore the whole folder'
	[ "$(stack_field demo-root deployed)" = "$revert" ] || fail "demo-root is not recorded as deployed at the revert commit $revert"
	[ "$(stack_images demo-root)" = "$image:1.1.0" ] || fail "demo-root does not run the previous image 1.1.0: $(stack_images demo-root)"
	log "scenario: demo-root, the root of /opt/stacks3, deployed 1.1.0 and rolled back 2.0.0 with the revert $revert"
}

# scenario_containers proves that qdeploy container list and show tell the three kinds apart on the host: the
# deployed demo stack is own, a Compose project from a folder outside the stacks repository is foreign, and a
# container started with docker run is single. The planted environment value must not show anywhere. What the
# scenario starts is removed before the answers are checked, so a failed check leaves nothing behind.
scenario_containers() {
	step 'container list shows own stacks, foreign projects and single containers'
	image=registry.$domain/demo/app:1.0.0
	foreign=$prefix-foreign single=$prefix-single planted=invented-planted-value-e2e
	foreign_dir=/tmp/$foreign
	docker exec "$host" sh -ec "rm -rf '$foreign_dir' && mkdir -p '$foreign_dir'" || fail 'cannot prepare the foreign project'
	printf 'services:\n  app:\n    image: %s\n    labels:\n      - %s\n' "$image" "$label" |
		docker exec -i "$host" sh -ec "cat >'$foreign_dir/compose.yaml'" || fail 'cannot write the compose file of the foreign project'
	docker exec "$host" docker rm -f "$single" >/dev/null 2>&1 || true
	docker exec "$host" docker run -d --name "$single" --label "$label" -e "E2E_PLANTED=$planted" "$image" >/dev/null ||
		fail 'cannot start the single container'
	docker exec "$host" docker compose -f "$foreign_dir/compose.yaml" -p "$foreign" up -d >/dev/null 2>&1 ||
		fail 'cannot start the foreign project'
	wait_for 'the single container' 30 health_is "$single" healthy
	wait_for 'the foreign project' 30 stack_healthy "$foreign"

	list_rc=0 show_rc=0 table_rc=0 missing_rc=0
	list_json=$(qd --output json container list) || list_rc=$?
	show_json=$(qd --output json container show "$single") || show_rc=$?
	list_table=$(qd container list) || table_rc=$?
	missing_out=$(qd container show "$prefix-no-such-container") || missing_rc=$?
	docker exec "$host" docker compose -f "$foreign_dir/compose.yaml" -p "$foreign" down >/dev/null 2>&1 || true
	docker exec "$host" sh -c "docker rm -f '$single' >/dev/null 2>&1; rm -rf '$foreign_dir'"

	[ "$list_rc" -eq 0 ] || fail "container list failed: $list_json"
	[ "$show_rc" -eq 0 ] || fail "container show failed: $show_json"
	[ "$table_rc" -eq 0 ] || fail "container list (table) failed: $list_table"
	[ "$missing_rc" -ne 0 ] || fail "container show of a missing container succeeded: $missing_out"
	printf '%s' "$list_json" | grep -q '"kind": *"own", *"name": *"demo-stack"' || fail "demo-stack is not an own group: $list_json"
	printf '%s' "$list_json" | grep -q "\"kind\": *\"foreign\", *\"name\": *\"$foreign\"" || fail "$foreign is not a foreign group: $list_json"
	printf '%s' "$list_json" | grep -q "\"working_dir\": *\"$foreign_dir\"" || fail "the foreign group does not show its working directory: $list_json"
	printf '%s' "$list_json" | grep -q "\"kind\": *\"single\", *\"name\": *\"$single\"" || fail "$single is not a single group: $list_json"
	printf '%s' "$list_table" | grep -q "^single  *$single " || fail "the table does not show $single as single: $list_table"
	printf '%s' "$show_json" | grep -q '"E2E_PLANTED"' || fail "container show does not name the planted variable: $show_json"
	printf '%s' "$show_json" | grep -q '"kind": *"single"' || fail "container show does not name the kind: $show_json"
	printf '%s' "$missing_out" | grep -q 'no such container' || fail "the refusal of a missing container is unclear: $missing_out"
	for text in "$list_json" "$show_json" "$list_table"; do
		if printf '%s' "$text" | grep -qF -- "$planted"; then fail 'an environment value shows in the container output'; fi
	done
	log 'scenario: container list showed own, foreign and single; the planted value stayed hidden'
}

# scenario_container_logs proves that qdeploy container logs reads one container and a whole stack, follows until
# interrupted with exit code 0, and shows the stack's secret only masked although the demo app prints it.
scenario_container_logs() {
	step 'container logs show a container and a stack, follow ends cleanly, secrets stay masked'
	secret=not-a-real-secret-e2e
	name=$(docker exec "$host" docker ps --filter label=com.docker.compose.project=demo-stack --format '{{.Names}}' | head -n 1)
	[ -n "$name" ] || fail 'no container of demo-stack runs'
	docker exec "$host" docker logs "$name" 2>&1 | grep -qF -- "$secret" || fail 'the demo app does not print its secret, so the mask cannot be checked'

	tail_rc=0 one_rc=0 stack_rc=0 follow_rc=0 json_rc=0 missing_rc=0
	tail_out=$(qd container logs "$name" --tail 20) || tail_rc=$?
	one_out=$(qd container logs "$name" --tail 0) || one_rc=$?
	stack_out=$(qd container logs --stack demo-stack --tail 0) || stack_rc=$?
	json_out=$(qd --output json container logs "$name" --tail 5) || json_rc=$?
	follow_out=$(docker exec "$host" timeout --preserve-status -s INT 6 qdeploy container logs "$name" --tail 1 --follow 2>&1 </dev/null) || follow_rc=$?
	missing_out=$(qd container logs --stack "$prefix-no-such-stack") || missing_rc=$?

	[ "$tail_rc" -eq 0 ] || fail "container logs --tail 20 failed: $tail_out"
	[ "$(printf '%s\n' "$tail_out" | grep -c .)" -ge 1 ] || fail "container logs --tail 20 shows no line: $tail_out"
	[ "$one_rc" -eq 0 ] || fail "container logs failed: $one_out"
	[ "$stack_rc" -eq 0 ] || fail "container logs --stack failed: $stack_out"
	[ "$json_rc" -eq 0 ] || fail "container logs (json) failed: $json_out"
	[ "$follow_rc" -eq 0 ] || fail "container logs --follow did not end with exit code 0 after the interrupt ($follow_rc): $follow_out"
	[ "$missing_rc" -ne 0 ] || fail "container logs of a missing stack succeeded: $missing_out"
	printf '%s' "$one_out" | grep -q 'demo started' || fail "container logs lacks the start line: $one_out"
	printf '%s' "$one_out" | grep -q '\[stderr\] demo warning on stderr' || fail "container logs does not mark stderr: $one_out"
	printf '%s' "$stack_out" | grep -q "$name .*demo started" || fail "the stack log does not name the container: $stack_out"
	printf '%s' "$json_out" | grep -q '"stream": *"stdout"' || fail "the json log does not name the stream: $json_out"
	[ "$(printf '%s\n' "$follow_out" | grep -c .)" -ge 2 ] || fail "--follow showed no new line within six seconds: $follow_out"
	printf '%s' "$missing_out" | grep -q 'no such stack' || fail "the refusal of a missing stack is unclear: $missing_out"
	for text in "$tail_out" "$one_out" "$stack_out" "$json_out" "$follow_out"; do
		if printf '%s' "$text" | grep -qF -- "$secret"; then fail 'a secret value shows in the container logs'; fi
	done
	log 'scenario: container logs read a container and a stack, followed until the interrupt; the secret stayed masked'
}

# scenario_container_actions proves that qdeploy container stop, start and restart act on a single container and
# that the question needs --yes without a terminal. The container is removed before the answers are checked.
scenario_container_actions() {
	step 'container stop, start and restart act on a single container'
	image=registry.$domain/demo/app:1.0.0
	single=$prefix-single-act
	docker exec "$host" docker rm -f "$single" >/dev/null 2>&1 || true
	docker exec "$host" docker run -d --name "$single" --label "$label" "$image" >/dev/null ||
		fail 'cannot start the single container'
	wait_for 'the single container' 30 health_is "$single" healthy

	refuse_rc=0 stop_rc=0 start_rc=0 restart_rc=0 missing_rc=0
	refuse_out=$(qd container stop "$single") || refuse_rc=$?
	refuse_state=$(qd --output json container show "$single")
	stop_out=$(qd container stop "$single" --yes) || stop_rc=$?
	stop_state=$(qd --output json container show "$single")
	start_out=$(qd container start "$single" --yes) || start_rc=$?
	start_state=$(qd --output json container show "$single")
	started_before=$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' "$single")
	restart_out=$(qd container restart "$single" --yes) || restart_rc=$?
	restart_state=$(qd --output json container show "$single")
	started_after=$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' "$single")
	missing_out=$(qd container stop "$prefix-no-such-container" --yes) || missing_rc=$?
	docker exec "$host" docker rm -f "$single" >/dev/null 2>&1

	[ "$refuse_rc" -ne 0 ] || fail "container stop without --yes succeeded: $refuse_out"
	printf '%s' "$refuse_out" | grep -q -- '--yes' || fail "the refusal does not name --yes: $refuse_out"
	printf '%s' "$refuse_state" | grep -q '"engine_state": *"running"' || fail "a refused stop changed the container: $refuse_state"
	[ "$stop_rc" -eq 0 ] || fail "container stop failed: $stop_out"
	printf '%s' "$stop_state" | grep -q '"state": *"stopped"' || fail "the container is not stopped after container stop: $stop_state"
	[ "$start_rc" -eq 0 ] || fail "container start failed: $start_out"
	printf '%s' "$start_state" | grep -q '"engine_state": *"running"' || fail "the container does not run after container start: $start_state"
	[ "$restart_rc" -eq 0 ] || fail "container restart failed: $restart_out"
	printf '%s' "$restart_state" | grep -q '"engine_state": *"running"' || fail "the container does not run after container restart: $restart_state"
	[ "$started_before" != "$started_after" ] || fail 'container restart did not restart the container'
	[ "$missing_rc" -ne 0 ] || fail "container stop of a missing container succeeded: $missing_out"
	printf '%s' "$missing_out" | grep -q 'no such container' || fail "the refusal of a missing container is unclear: $missing_out"
	log 'scenario: container stop, start and restart acted on a single container'
}

# field_is succeeds when a flat field of a stack in qdeploy status has the given value.
field_is() {
	[ "$(stack_field "$1" "$2")" = "$3" ]
}

# running_containers prints how many containers of the stack run.
running_containers() {
	docker exec "$host" docker ps -q --filter "label=com.docker.compose.project=$1" | wc -l
}

# scenario_stack_actions proves that a stop through qdeploy keeps demo-cache stopped against a new commit and
# the service restart that makes the poll run, that only a start clears the mark and lets the next poll deploy
# the commit, and that a restart restarts demo-stack. Both stacks run again at the end.
scenario_stack_actions() {
	step 'stack stop asks first, stops demo-cache and shows the mark in status and stack list'
	refuse_rc=0
	refuse_out=$(qd stack stop demo-cache) || refuse_rc=$?
	[ "$refuse_rc" -ne 0 ] || fail "stack stop without --yes succeeded: $refuse_out"
	printf '%s' "$refuse_out" | grep -q -- '--yes' || fail "the refusal does not name --yes: $refuse_out"
	[ "$(running_containers demo-cache)" -gt 0 ] || fail 'a refused stack stop stopped demo-cache'
	out=$(qd stack stop demo-cache --yes) || fail "stack stop failed: $out"
	[ "$(running_containers demo-cache)" -eq 0 ] || fail 'demo-cache still runs after stack stop'
	[ -n "$(docker exec "$host" docker ps -aq --filter label=com.docker.compose.project=demo-cache)" ] || fail 'stack stop removed the containers of demo-cache'
	[ -n "$(stack_field demo-cache stopped)" ] || fail "status does not show demo-cache as stopped: $(qd --output json status)"
	qd stack list | grep demo-cache | grep -q 'stopped since' || fail "stack list does not show demo-cache as stopped: $(qd stack list)"
	[ -z "$(stack_field demo-stack stopped)" ] || fail 'demo-stack is shown as stopped'
	out=$(qd stack restart demo-cache --yes) && fail "stack restart of a stopped stack succeeded: $out"
	printf '%s' "$out" | grep -q 'qdeploy stack start' || fail "the refusal does not name stack start: $out"

	out=$(qd stack deploy demo-cache) && fail "stack deploy of a stopped stack succeeded: $out"
	printf '%s' "$out" | grep -q 'qdeploy stack start' || fail "the refusal of stack deploy does not name stack start: $out"
	[ "$(running_containers demo-cache)" -eq 0 ] || fail 'a refused stack deploy started demo-cache'

	step 'a new commit does not start a run of the stopped stack, also not after a service restart'
	count=$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')
	as_admin 'cd ~/stacks &&
		printf "# stopped-stack marker\n" >>demo-cache/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Touch demo-cache while it is stopped" &&
		git push -q origin main'
	pushed=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	wait_for 'the poll after the restart' 60 poll_done
	wait_for 'the pending commit of demo-cache' 60 field_is demo-cache pending "$pushed"
	[ "$(qd stack runs demo-cache | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-')" = "$count" ] || fail 'a run of the stopped stack was started by the poll'
	[ "$(running_containers demo-cache)" -eq 0 ] || fail 'demo-cache runs again after a new commit'
	[ -n "$(stack_field demo-cache stopped)" ] || fail 'the stopped mark did not survive the service restart'
	stack_field demo-cache findings | grep -q stopped || fail "demo-cache has no finding stopped: $(qd --output json status)"

	step 'stack start clears the mark, starts demo-cache and the next poll deploys the commit'
	out=$(qd stack start demo-cache --yes) || fail "stack start failed: $out"
	[ -z "$(stack_field demo-cache stopped)" ] || fail "status still shows demo-cache as stopped: $(qd --output json status)"
	[ "$(running_containers demo-cache)" -gt 0 ] || fail 'demo-cache does not run after stack start'
	stack_healthy demo-cache || fail 'demo-cache is not healthy after stack start'
	restart_service
	wait_for 'the run of demo-cache after the start' 240 stack_idle demo-cache
	wait_for 'the commit deployed on demo-cache' 240 field_is demo-cache deployed "$pushed"
	stack_healthy demo-cache || fail 'demo-cache is not healthy after the run'
	log "scenario: demo-cache stayed stopped against commit $pushed until stack start"

	step 'stack restart restarts demo-stack'
	ids=$(docker exec "$host" docker ps -q --filter label=com.docker.compose.project=demo-stack)
	started_before=$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' $ids)
	out=$(qd stack restart demo-stack --yes) || fail "stack restart failed: $out"
	started_after=$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' $ids)
	[ "$started_before" != "$started_after" ] || fail 'stack restart did not restart demo-stack'
	stack_healthy demo-stack || fail 'demo-stack is not healthy after stack restart'
	out=$(qd stack stop nope --yes) && fail "stack stop of an unknown stack succeeded: $out"
	printf '%s' "$out" | grep -q 'no such stack' || fail "the refusal of an unknown stack is unclear: $out"
	log 'scenario: stack stop, start and restart passed'
}

# has_finding succeeds when the stack shows the finding $2 in qdeploy status.
has_finding() {
	stack_field "$1" findings | grep -q "$2"
}

# no_finding succeeds when the stack does not show the finding $2.
no_finding() {
	! has_finding "$1" "$2"
}

# scenario_container_monitor proves that the failure monitor reports a container of demo-stack that dies outside
# qdeploy and then repairs it: the webhook hears container-failed and qdeploy status shows the finding; qdeploy
# restarts the container after a minute, and the webhook hears recovered with restarted. The same for a container
# that turns unhealthy. The restart policy is switched off for the kill, so only qdeploy can bring the container
# back; the monitor reads every 30 seconds and ends a failure after two clean readings.
scenario_container_monitor() {
	step 'a channel for the failure monitor events is added'
	out=$(qd_in "$hook_url
$hook_signing_value
" notify add mon --type webhook --events container-failed,recovered) || fail "notify add failed: $out"
	qd notify list | grep -q 'mon *webhook *container-failed,recovered' || fail "notify list does not show the channel: $(qd notify list)"

	step 'a container killed outside qdeploy is reported and shown in status'
	wait_for 'demo-stack idle' 120 stack_idle demo-stack
	stack_healthy demo-stack || fail 'demo-stack is not healthy before the kill'
	no_finding demo-stack container-failed || fail "demo-stack shows a failure before the kill: $(qd --output json status)"
	victim=$(docker exec "$host" docker ps -q --filter label=com.docker.compose.project=demo-stack | head -n 1)
	[ -n "$victim" ] || fail 'demo-stack has no container to kill'
	docker exec "$host" docker update --restart=no "$victim" >/dev/null || fail 'cannot switch off the restart policy'
	docker exec "$host" docker kill "$victim" >/dev/null || fail 'cannot kill the container'
	wait_for 'the webhook for the failed container' 120 hook_seen /notify container-failed demo-stack
	wait_for 'the finding container-failed in status' 60 has_finding demo-stack container-failed
	stack_field demo-stack findings | grep -q 'exited' || fail "the finding does not name the kind exited: $(qd --output json status)"
	log 'scenario: the killed container was reported and shown as a finding'

	step 'qdeploy restarts the container: recovered is reported with restarted and the finding ends'
	wait_for 'demo-stack healthy again' 150 stack_healthy demo-stack
	wait_for 'the webhook for the recovered container' 240 hook_has /notify recovered '\"stack\":\"demo-stack\"' '\"kind\":\"exited\"' '\"restarted\":true'
	wait_for 'the finding to end' 60 no_finding demo-stack container-failed
	docker exec "$host" docker update --restart=unless-stopped "$victim" >/dev/null || fail 'cannot restore the restart policy'
	log 'scenario: qdeploy restarted the killed container and the finding ended'

	step 'a container that turns unhealthy is restarted by qdeploy'
	docker exec "$host" touch /opt/stacks/demo-stack/data/break-once || fail 'cannot break the health of demo-stack'
	wait_for 'the webhook for the unhealthy container' 240 hook_has /notify container-failed '\"stack\":\"demo-stack\"' '\"kind\":\"unhealthy\"'
	wait_for 'the finding container-failed in status' 60 has_finding demo-stack unhealthy
	wait_for 'the webhook for the recovered container' 360 hook_has /notify recovered '\"stack\":\"demo-stack\"' '\"kind\":\"unhealthy\"' '\"restarted\":true'
	wait_for 'the finding to end' 60 no_finding demo-stack container-failed
	stack_healthy demo-stack || fail 'demo-stack is not healthy after the repair'
	if docker exec "$host" test -e /opt/stacks/demo-stack/data/break-once; then fail 'the restart did not clear the break'; fi
	out=$(qd notify remove mon) || fail "notify remove failed: $out"
	log 'scenario: the unhealthy container was restarted by qdeploy and recovered'
}

# repair_text prints the repair line of a stack in qdeploy status.
repair_text() {
	stack_field "$1" repair
}

repair_first_attempt() {
	repair_text repair-demo | grep -q '^1 of 3 attempts, next '
}

repair_halted() {
	repair_text "$1" | grep -q '^halted since '
}

# scenario_repair_start adds the stack repair-demo and breaks it for good: its health check fails while the file
# data/break exists, which a restart does not remove. qdeploy restarts it 1, 5 and 15 minutes after the failure
# began and then halts. The delays run in real time; scenario_repair_end checks the outcome at the end of the
# run, so they overlap with the other scenarios.
scenario_repair_start() {
	step 'the stack repair-demo is deployed and a channel for recovery-failed is added'
	as_admin 'cd ~/stacks && git pull -q --ff-only && mkdir repair-demo'
	cat <<EOF | as_admin 'cat >"$HOME"/stacks/repair-demo/compose.yaml'
services:
  app:
    image: registry.$domain/demo/app:1.0.0
    restart: unless-stopped
    environment:
      DEMO_NAME: repair-demo
    volumes:
      - ./data:/data
EOF
	as_admin 'cd ~/stacks && cp demo-stack/.gitignore repair-demo/.gitignore && git add -A &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -m "Add the repair-demo stack" && git push -q origin main'
	restart_service
	wait_for 'the first run of repair-demo' 240 stack_settled repair-demo succeeded
	wait_for 'repair-demo healthy' 60 stack_healthy repair-demo
	out=$(qd_in "$hook_url
$hook_signing_value
" notify add rep --type webhook --events recovered,recovery-failed) || fail "notify add rep failed: $out"
	[ -z "$(repair_text repair-demo)" ] || fail "a healthy stack shows a repair: $(repair_text repair-demo)"

	step 'repair-demo turns unhealthy for good; the first attempt shows in status'
	docker exec "$host" touch /opt/stacks/repair-demo/data/break || fail 'cannot break repair-demo'
	repair_demo_broken=$(date +%s)
	wait_for 'the finding container-failed of repair-demo' 240 has_finding repair-demo container-failed
	wait_for 'the first repair attempt' 240 repair_first_attempt
	log "scenario: repair-demo $(repair_text repair-demo)"
}

# scenario_repair_end waits until qdeploy gave up on repair-demo and proves that the halt is shown, reported
# once and released by resume; then it fixes the stack and removes it from the repository.
scenario_repair_end() {
	step 'after three attempts the repair is halted, shown in status and the TUI, and reported once'
	wait_for 'the repair of repair-demo to halt' 1800 repair_halted repair-demo
	log "scenario: repair-demo halted after $(($(date +%s) - repair_demo_broken)) seconds: $(repair_text repair-demo)"
	repair_text repair-demo | grep -q 'after 3 of 3 attempts' || fail "status does not name the attempts: $(repair_text repair-demo)"
	wait_for 'the webhook for the failed recovery' 60 hook_has /notify recovery-failed '\"stack\":\"repair-demo\"' '\"attempts\":3'
	sleep 35
	[ "$(docker exec "$prefix-hooks" cat /data/requests.jsonl | grep -F '"path":"/notify"' | grep -F '"X-Qdeploy-Event":"recovery-failed"' | grep -cF '\"stack\":\"repair-demo\"')" = 1 ] ||
		fail 'recovery-failed was not sent exactly once'
	tui_tmux kill-server >/dev/null 2>&1 || true
	tui_tmux new-session -d -s qdtui -x 120 -y 40 "env LANG=C.UTF-8 sh -c 'sudo qdeploy; echo qdeploy-exit=\$?; sleep 600'" ||
		fail 'cannot start tmux for the interface'
	tui_wait 'repair-demo' 30
	tui_wait 'halted' 10
	tui_tmux kill-server >/dev/null 2>&1 || true

	step 'resume releases the halted repair and prints no pause time'
	docker exec "$host" rm -f /opt/stacks/repair-demo/data/break
	out=$(qd --output json stack resume repair-demo) || fail "stack resume failed: $out"
	printf '%s' "$out" | grep -q '"repair": *"released"' || fail "resume does not say that the repair was released: $out"
	if printf '%s' "$out" | grep -q '0001-01-01\|paused_since'; then fail "resume prints a pause time: $out"; fi
	if repair_halted repair-demo; then fail "status still shows the repair as halted: $(repair_text repair-demo)"; fi
	if qd stack resume repair-demo >/dev/null; then fail 'a second resume succeeded'; fi

	step 'repair-demo is healthy again and leaves the repository'
	wait_for 'repair-demo healthy' 120 stack_healthy repair-demo
	wait_for 'the finding of repair-demo to end' 120 no_finding repair-demo container-failed
	out=$(qd notify remove rep) || fail "notify remove rep failed: $out"
	as_admin 'cd ~/stacks && git pull -q --ff-only && git rm -r -q repair-demo &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -m "Remove the repair-demo stack" && git push -q origin main'
	log 'scenario: resume released the repair; repair-demo is fixed and removed'
}

# selfupdate_field prints one top-level field of qdeploy update status, run without root.
selfupdate_field() {
	as_admin "qdeploy --output json update status 2>&1" | sed -n "s/.*\"$1\": *\"\{0,1\}\([^\",}]*\).*/\1/p"
}

# service_pid prints the main process ID of the qdeploy service.
service_pid() {
	docker exec "$host" systemctl show -p MainPID --value qdeploy
}

# service_entered prints when the service last became active.
service_entered() {
	docker exec "$host" systemctl show -p ActiveEnterTimestampMonotonic --value qdeploy
}

# installed_sum identifies the installed program and the unit systemd uses.
installed_sum() {
	docker exec "$host" sh -c 'sha256sum /usr/local/bin/qdeploy; systemctl cat qdeploy | sha256sum'
}

service_serving() {
	[ "$(docker exec "$host" systemctl is-active qdeploy)" = active ] && qd status >/dev/null
}

# scenario_selfupdate updates the installed service to a newer signed test release and refuses tampered ones.
scenario_selfupdate() {
	docker exec "$host" systemctl start qdeploy || fail 'cannot start the qdeploy service'
	wait_for 'the qdeploy service to answer' 30 service_serving

	step 'update status reports the newer test release without root'
	release_publish v0.2.0
	[ "$(docker exec "$host" qdeploy version 2>&1 | grep -c v0.1.0)" -ge 1 ] || fail "the installed version is not v0.1.0: $(docker exec "$host" qdeploy version 2>&1)"
	[ "$(selfupdate_field installed)" = v0.1.0 ] || fail "update status: $(as_admin 'qdeploy --output json update status 2>&1')"
	[ "$(selfupdate_field latest)" = v0.2.0 ] || fail "update status latest: $(as_admin 'qdeploy --output json update status 2>&1')"
	[ "$(selfupdate_field update_available)" = true ] || fail "update status: $(as_admin 'qdeploy --output json update status 2>&1')"
	log "scenario: update status: $(as_admin 'qdeploy --output json update status 2>&1' | tr -d '\n ')"

	step 'update without root and without --confirm changes nothing'
	before=$(installed_sum)
	if out=$(as_admin 'qdeploy update --yes --confirm 2>&1'); then fail "update without root succeeded: $out"; fi
	if out=$(as_admin 'sudo qdeploy update --yes 2>&1'); then fail "update without --confirm succeeded: $out"; fi
	[ "$(installed_sum)" = "$before" ] || fail 'a refused update changed the installation'

	step 'sudo qdeploy update installs the newer release and restarts the service'
	pid=$(service_pid) entered=$(service_entered)
	out=$(as_admin 'sudo qdeploy update --yes --confirm 2>&1') || fail "update failed: $out"
	wait_for 'the updated service' 60 service_serving
	[ "$(service_pid)" != "$pid" ] && [ "$(service_entered)" != "$entered" ] || fail 'the service was not restarted by the update'
	docker exec "$host" qdeploy version 2>&1 | grep -q v0.2.0 || fail "the installed version is not v0.2.0: $(docker exec "$host" qdeploy version 2>&1)"
	[ "$(selfupdate_field installed)" = v0.2.0 ] && [ "$(selfupdate_field update_available)" = false ] ||
		fail "update status after the update: $(as_admin 'qdeploy --output json update status 2>&1')"
	[ "$(docker exec "$host" cat /etc/systemd/system/qdeploy.service.d/e2e.conf | grep -c backup-image)" = 1 ] || fail 'the update removed the drop-in'
	docker exec "$host" grep -qx 'ReadWritePaths=-/opt/stacks' /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf ||
		fail 'the update removed the stack folder grant'
	docker exec "$host" systemctl show -p ReadWritePaths qdeploy | grep -qF /opt/stacks || fail 'the updated service has no write access to /opt/stacks'
	log "scenario: updated to v0.2.0, pid $pid -> $(service_pid), service answering: $out"

	step 'a release with a tampered checksums.txt is refused and changes nothing'
	release_build v0.3.0
	good=$work/releases/v0.3.0
	bad=$work/releases/v0.3.0-tampered
	rm -rf "$bad"
	cp -r "$good" "$bad"
	printf '# tampered\n' >>"$bad/checksums.txt"
	cmp -s "$good/checksums.txt" "$bad/checksums.txt" && fail 'the test did not tamper with checksums.txt'
	release_publish v0.3.0 "$bad"
	refuse_update v0.3.0

	step 'a release with a tampered archive is refused and changes nothing'
	rm -rf "$bad"
	cp -r "$good" "$bad"
	printf 'x' >>"$bad/qdeploy_v0.3.0_linux_$(docker exec "$host" dpkg --print-architecture).tar.gz"
	release_publish v0.3.0 "$bad"
	refuse_update v0.3.0

	step 'a release signed by an unknown key is refused and changes nothing'
	rm -rf "$bad"
	cp -r "$good" "$bad"
	ssh-keygen -q -t ed25519 -N '' -C "$prefix-unknown-key" -f "$work/keys/unknown"
	rm -f "$bad/$signature_asset"
	(cd "$bad" && ssh-keygen -q -Y sign -f "$work/keys/unknown" -n qdeploy-release checksums.txt)
	release_publish v0.3.0 "$bad"
	refuse_update v0.3.0

	docker exec "$host" systemctl stop qdeploy
}

# refuse_update requires that sudo qdeploy update fails for the release $1 and that program, unit, version and
# service process stay as they were.
refuse_update() {
	before=$(installed_sum) pid=$(service_pid) entered=$(service_entered)
	if out=$(as_admin 'sudo qdeploy update --yes --confirm 2>&1'); then fail "update accepted a tampered release: $out"; fi
	[ "$(installed_sum)" = "$before" ] || fail 'a refused update changed the installation'
	[ "$(service_pid)" = "$pid" ] && [ "$(service_entered)" = "$entered" ] || fail 'a refused update restarted the service'
	docker exec "$host" qdeploy version 2>&1 | grep -q v0.2.0 || fail 'a refused update changed the version'
	service_serving || fail 'the service does not answer after a refused update'
	log "scenario: tampered release $1 refused, installation unchanged: $out"
}

# window_field prints one field of qdeploy window show (empty when it is absent).
window_field() {
	qd --output json window show | sed -n "s/.*\"$1\": *\"\{0,1\}\([^\",}]*\).*/\1/p"
}

# window_far_away opens the window daily at the time twelve hours from now, for one minute, and prints the answer.
window_far_away() {
	qd --output json window set --days daily --zone UTC --start "$(date -u -d '+12 hours' +%H:%M)" --duration 1m
}

# window_log prints what the service logged about the window and the stacks since it started.
window_log() {
	docker exec "$host" journalctl -u qdeploy --no-pager -o cat
}

# stack_run_count prints the number of recorded runs of a stack.
stack_run_count() {
	qd stack runs "$1" | grep -c '[0-9]\{8\}T[0-9]\{6\}Z-' || true
}

# window_done succeeds when the service has begun the work of an opening since last_run read $window_last_before
# and has finished it. The default window may already have run once when the service started.
window_done() {
	last=$(window_field last_run)
	[ -n "$last" ] && [ "$last" != "$window_last_before" ] && [ -z "$(window_field running)" ]
}

# scenario_window proves the maintenance window with the real service and a real, short window. demo-stack is in
# automatic mode, demo-cache in manual mode, demo-paused is paused and demo-pinned is pinned, all four offer 1.1.0.
scenario_window() {
	image=registry.$domain/demo/app
	win_stacks='demo-stack demo-cache demo-paused demo-pinned'

	step 'four stacks wait for 1.1.0: automatic, manual, paused and pinned'
	prev_stack=$(newest_run demo-stack) prev_cache=$(newest_run demo-cache)
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	demo_stack_settings 'update:\n  mode: automatic\n' | as_admin 'cat >"$HOME"/stacks/.qatlas/qdeploy/demo-stack.yaml'
	as_admin 'cd ~/stacks &&
		for s in demo-paused demo-pinned; do
			cp -r demo-cache $s && sed -i "s/demo-cache/$s/" $s/compose.yaml &&
			sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.0.0/" $s/.env &&
			printf "version: 1\nupdate:\n  mode: automatic\n" >.qatlas/qdeploy/$s.yaml
		done &&
		printf "version: 1\nupdate:\n  mode: manual\n" >.qatlas/qdeploy/demo-cache.yaml &&
		sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.0.1/" demo-stack/.env demo-cache/.env &&
		git add -A && git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -m "Prepare the maintenance window stacks" &&
		git push -q origin main'
	setup_commit=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	for pair in "demo-stack $prev_stack" "demo-cache $prev_cache"; do
		set -- $pair
		run=$(next_run "$1" "$2") || fail "no run of $1 started for the window setup commit"
		wait_for "the run $run" 240 run_finished "$1" "$run"
		qd stack log "$run" | grep -q ' result succeeded' || fail "the run of $1 for the window setup did not succeed: $(qd stack log "$run")"
	done
	for stack in demo-paused demo-pinned; do
		wait_for "the first run of $stack" 120 stack_idle "$stack"
		docker exec -u qdeploy "$host" sh -ec "umask 077 && printf 'DEMO_SECRET=%s\n' not-a-real-secret-e2e >/opt/stacks/$stack/secrets.env"
		out=$(qd --output json stack deploy "$stack") || fail "deploy of $stack failed: $out"
		run=$(printf '%s\n' "$out" | sed -n 's/.*"run": *"\([^"]*\)".*/\1/p')
		wait_for "the run $run" 240 run_finished "$stack" "$run"
		qd stack log "$run" | grep -q ' result succeeded' || fail "the deploy of $stack did not succeed: $(qd stack log "$run")"
	done
	[ "$(stack_images demo-stack)" = "$image:1.0.1" ] && [ "$(stack_images demo-cache)" = "$image:1.0.1" ] &&
		[ "$(stack_images demo-paused)" = "$image:1.0.0" ] && [ "$(stack_images demo-pinned)" = "$image:1.0.0" ] ||
		fail "the window stacks do not run their starting versions: $(for s in $win_stacks; do stack_images $s; done | tr '\n' ' ')"

	step 'pause and pin mark the stacks'
	out=$(qd --output json stack pause demo-paused) || fail "pause failed: $out"
	out=$(qd --output json stack pin demo-pinned) || fail "pin failed: $out"
	[ -n "$(stack_field demo-paused operator_pause)" ] || fail "status does not show demo-paused as paused: $(qd --output json status)"
	[ -n "$(stack_field demo-pinned pinned)" ] || fail "status does not show demo-pinned as pinned: $(qd --output json status)"
	[ -z "$(stack_field demo-stack operator_pause)$(stack_field demo-stack pinned)$(stack_field demo-cache operator_pause)$(stack_field demo-cache pinned)" ] ||
		fail 'a stack without a mark shows one'

	step 'window set takes effect in the running service without a restart'
	started=$(status_field started_at)
	[ -n "$started" ] || fail 'status shows no start time of the service'
	# --start has minute granularity: two minutes ahead opens the window between one and two minutes from now.
	start=$(date -u -d '+2 minutes' +%H:%M)
	out=$(qd --output json window set --days daily --zone UTC --start "$start" --duration 5m) || fail "window set failed: $out"
	log "scenario: window set: $out"
	[ "$(window_field start)" = "$start" ] && [ "$(window_field duration)" = 5m ] && [ "$(window_field zone)" = UTC ] ||
		fail "window show does not report the new window: $(qd --output json window show)"
	[ "$(window_field open)" = false ] || fail "the window is open before its start: $(qd --output json window show)"
	case $(window_field next_opening) in *T$start:00Z) ;; *) fail "the next opening is not at $start UTC: $(qd --output json window show)" ;; esac
	window_last_before=$(window_field last_run)

	for stack in $win_stacks; do
		eval "runs_$(printf '%s' "$stack" | tr - _)=$(stack_run_count "$stack")"
		eval "deployed_$(printf '%s' "$stack" | tr - _)=$(stack_field "$stack" deployed)"
	done
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	[ "$head_before" = "$setup_commit" ] || fail "the stacks repository moved after the setup commit: $head_before"

	step 'the window opens and updates the automatic stack by itself'
	wait_for 'the window to open' 150 sh -c "docker exec -i $host qdeploy --output json window show | grep -q '\"open\": *true'"
	log "scenario: window open: $(qd --output json window show | tr -d '\n ')"
	wait_for 'the window work to finish' 300 window_done
	[ "$(window_field open)" = true ] || fail "the window work ran past the end of the window: $(qd --output json window show)"
	[ "$(status_field started_at)" = "$started" ] || fail 'the service restarted'
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "the automatic stack does not run 1.1.0: $(stack_images demo-stack)"
	stack_healthy demo-stack || fail 'demo-stack is not healthy after the window update'
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 1 ] || fail 'the window did not make exactly one commit'
	update_commit=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$update_commit" ] || fail 'the update commit is not on the remote branch'
	update_commit_only_version "$update_commit" 1.0.1 1.1.0
	[ "$(stack_field demo-stack deployed)" = "$update_commit" ] || fail "demo-stack is not recorded as deployed at $update_commit"
	[ "$(stack_run_count demo-stack)" = "$((runs_demo_stack + 1))" ] || fail 'the window did not make exactly one run of demo-stack'
	log "scenario: window updated demo-stack to 1.1.0 with commit $update_commit"

	step 'the manual stack is only reported'
	[ "$(stack_images demo-cache)" = "$image:1.0.1" ] || fail "the manual stack changed: $(stack_images demo-cache)"
	[ "$(stack_run_count demo-cache)" = "$runs_demo_cache" ] || fail 'the window ran the manual stack'
	[ "$(stack_field demo-cache deployed)" = "$deployed_demo_cache" ] || fail 'the window changed the deployed commit of the manual stack'
	[ -z "$(as_admin "cd ~/stacks && git log --format=%H $setup_commit..HEAD -- demo-cache")" ] || fail 'the window committed to the manual stack'
	out=$(qd --output json stack updates demo-cache) || fail "updates demo-cache failed: $out"
	updates_expect "$out" 1.0.1 1.1.0 5.0.0 update
	notice=$(window_log | grep 'event=update-available' | grep 'stack=demo-cache' || true)
	[ -n "$notice" ] || fail "the service log has no update-available line for demo-cache: $(window_log | tail -n 20)"
	log "scenario: log: $(printf '%s' "$notice" | tail -n 1)"

	step 'the paused and the pinned stack stay unchanged, their update stays visible'
	for stack in demo-paused demo-pinned; do
		v=$(printf '%s' "$stack" | tr - _)
		[ "$(stack_images "$stack")" = "$image:1.0.0" ] || fail "$stack changed: $(stack_images "$stack")"
		[ "$(stack_run_count "$stack")" = "$(eval "printf %s \"\$runs_$v\"")" ] || fail "the window ran $stack"
		[ "$(stack_field "$stack" deployed)" = "$(eval "printf %s \"\$deployed_$v\"")" ] || fail "the window changed the deployed commit of $stack"
		[ -z "$(as_admin "cd ~/stacks && git log --format=%H $setup_commit..HEAD -- $stack")" ] || fail "the window committed to $stack"
		out=$(qd --output json stack updates "$stack") || fail "updates $stack failed: $out"
		updates_expect "$out" 1.0.0 1.1.0 5.0.0 update
		window_log | grep 'stack skipped' | grep "stack=$stack" | grep -q 'reason=' || fail "the service log does not say why $stack was skipped"
		log "scenario: log: $(window_log | grep 'stack skipped' | grep "stack=$stack" | tail -n 1)"
	done

	step 'the marks are lifted and the window is moved away'
	qd stack unpause demo-paused >/dev/null || fail 'unpause failed'
	qd stack unpin demo-pinned >/dev/null || fail 'unpin failed'
	out=$(window_far_away) || fail "window set failed: $out"
	[ "$(window_field open)" = false ] || fail "the window is still open after it was moved: $(qd --output json window show)"
}

# The terminal interface is driven in a tmux pane of the administrator, 120 columns by 40 rows. The command
# keeps the pane after qdeploy ends, so the exit code can be read from the screen.
tui_tmux() {
	docker exec -u "$admin" "$host" tmux -L qdtui "$@"
}

# tui_screen prints the screen and appends it to the capture log, which the first-setup section searches for
# masked values at its end.
tui_screen() {
	tui_tmux capture-pane -p -t qdtui | tee -a "${tui_log:-/dev/null}" 2>/dev/null
}

tui_send() {
	tui_tmux send-keys -t qdtui "$@"
}

tui_has() {
	tui_screen | grep -qF -- "$1"
}

# tui_wait waits up to $2 seconds for the text $1 on the screen and shows the screen when it does not appear.
tui_wait() {
	i=0
	until tui_has "$1"; do
		i=$((i + 1))
		if [ "$i" -ge $(($2 * 4)) ]; then
			printf '%s\n' "$(tui_screen)" >&2
			fail "timeout waiting for '$1' on the screen of the terminal interface"
		fi
		sleep 0.25
	done
}

# tui_cursor_to moves the cursor of the Stacks list to the stack $1.
tui_cursor_to() {
	i=0
	until tui_screen | grep -q "^> .*\[.\] $1 "; do
		i=$((i + 1))
		[ "$i" -lt 12 ] || fail "the cursor does not reach $1: $(tui_screen)"
		tui_send Down
		sleep 0.2
	done
}

# tui_click presses and releases the left mouse button at the first cell of the text $1 on the screen, as a
# terminal reports it (SGR, 1-based), and fails when the text is not on the screen. The text must lie where
# only ASCII precedes it on its line.
tui_click() {
	cell=$(tui_screen | awk -v t="$1" 'i = index($0, t) { print i, NR; exit }')
	[ -n "$cell" ] || fail "cannot click '$1', it is not on the screen: $(tui_screen)"
	set -- $cell
	tui_send -l "$(printf '\033[<0;%d;%dM\033[<0;%d;%dm' "$1" "$2" "$1" "$2")"
}

# tui_click_row clicks the stack $1 in the Stacks list and waits until the cursor marks it.
tui_click_row() {
	tui_click "$1"
	i=0
	until tui_screen | grep -q "^> .*\[.\] $1 "; do
		i=$((i + 1))
		[ "$i" -lt 20 ] || fail "the click did not mark $1: $(tui_screen)"
		sleep 0.25
	done
}

# tui_excerpt logs the lines of the screen from the first one containing $1, at most $2 lines.
tui_excerpt() {
	tui_screen | sed -n "/$1/,\$p" | head -n "$2" | sed 's/ *$//; s/^/e2e:   | /'
}

# scenario_tui drives the terminal interface of the running service the way an administrator does. demo-cache
# (manual mode, 1.0.1) is updated alone with the changelog of its image; demo-paused and demo-pinned (1.0.0) are
# ticked and updated together. All three offer 1.1.0 after the window section.
scenario_tui() {
	image=registry.$domain/demo/app

	step 'demo-cache gets a changelog source and a fresh update check'
	settings_set demo-cache "$(printf 'version: 1\nupdate:\n  mode: manual\n  changelog:\n    DEMO_APP_VERSION: qdeploye2e-org/demo-image\n')"
	printf '%s' "$settings_out" | grep -q '"changed": *true' || fail "settings set did not add the changelog source: $settings_out"
	qd stack updates --refresh >/dev/null || fail 'updates --refresh failed'
	out=$(qd stack changelog demo-cache) || fail "changelog failed: $out"
	printf '%s' "$out" | grep -q 'invented crash' || fail "the changelog of demo-cache has no release notes: $out"

	step 'sudo qdeploy opens the interface and lists the demo stacks'
	tui_tmux kill-server >/dev/null 2>&1 || true
	tui_tmux new-session -d -s qdtui -x 120 -y 40 "env LANG=C.UTF-8 sh -c 'sudo qdeploy; echo qdeploy-exit=\$?; sleep 600'" ||
		fail 'cannot start tmux for the interface'
	tui_wait '1 Stacks' 30
	for name in demo-stack demo-cache demo-paused demo-pinned; do tui_wait "$name" 30; done
	tui_wait 'q quit' 10
	log 'scenario: interface open, stacks listed'

	step 'a click on a tab switches the area and a click on a stack row marks it'
	tui_click '3 Runs'
	tui_wait 'TRIGGER' 10
	tui_click '1 Stacks'
	tui_wait 'CURRENT' 10
	tui_click_row demo-pinned
	# tui_cursor_to only moves down, so the cursor goes back to the first row.
	tui_click_row bookstack
	log 'scenario: mouse clicks switched the area and marked a row'

	step 'u asks first: the question shows the versions and the changelog, nothing changes yet'
	head_before=$(as_admin 'cd ~/stacks && git pull -q --ff-only && git rev-parse HEAD')
	runs_before=$(stack_run_count demo-cache)
	tui_cursor_to demo-cache
	tui_send u
	tui_wait 'Update demo-cache?' 15
	tui_wait '1.0.1 -> 1.1.0' 15
	tui_wait 'Fixes an invented crash on empty input.' 30
	log 'scenario: update question of demo-cache:'
	tui_excerpt "Update demo-cache" 14
	[ "$(stack_images demo-cache)" = "$image:1.0.1" ] || fail "demo-cache changed before the answer: $(stack_images demo-cache)"
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$head_before" ] || fail 'the question already made a commit'
	[ "$(stack_run_count demo-cache)" = "$runs_before" ] || fail 'the question already started a run'

	step 'y runs the update; while it runs the Runs area lists the run and opens its log, which follows it'
	tui_send y
	tui_send 3
	# The list is read again on r. The run lasts a few seconds, long enough for a poll every 0.3 seconds.
	i=0
	until tui_screen | grep -qE 'demo-cache +[0-9-]+ +[0-9:]+ +running'; do
		i=$((i + 1))
		[ "$i" -lt 30 ] || fail "the Runs area never listed the running update: $(tui_screen)"
		tui_send r
		sleep 0.3
	done
	tui_send Home Enter
	tui_wait 'following' 10
	tui_screen | grep -qE '^ +demo-cache +running +[0-9]{8}T' || fail "the opened log is not that of the running update: $(tui_screen)"
	tui_wait 'step ' 10
	log 'scenario: live run log of the update:'
	tui_excerpt 'demo-cache  ' 8
	tui_wait 'update finished: 1 ok, 0 failed' 120

	step 'l opens the log of the finished run; the update made one commit and the stack runs 1.1.0'
	tui_send l
	tui_wait 'result succeeded' 15
	screen_run=$(tui_screen | grep -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}' | head -n 1)
	[ -n "$screen_run" ] && [ "$screen_run" = "$(newest_run demo-cache)" ] ||
		fail "the log on the screen is not the newest run of demo-cache ($screen_run): $(tui_screen)"
	run_log=$(qd stack log "$screen_run")
	printf '%s' "$run_log" | grep -q ' result succeeded' || fail "the update run did not succeed: $run_log"
	[ "$(update_steps "$run_log")" = stop,backup,commit,push,fast-forward,pull,up,check ] || fail "unexpected steps of the update run: $(update_steps "$run_log")"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 1 ] || fail 'the update did not create exactly one commit'
	tui_commit_only_version "$(as_admin 'cd ~/stacks && git rev-parse HEAD')" demo-cache 1.0.1 1.1.0
	[ "$(stack_images demo-cache)" = "$image:1.1.0" ] || fail "demo-cache does not run 1.1.0: $(stack_images demo-cache)"
	stack_healthy demo-cache || fail 'demo-cache is not healthy after the update'
	log "scenario: interface update of demo-cache: run $screen_run, demo-cache runs 1.1.0"

	step 'two ticked stacks are updated with one question, one after another'
	head_before=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	tui_send 1
	tui_wait '[ ] demo-paused' 10
	tui_cursor_to demo-paused
	tui_send Space
	tui_cursor_to demo-pinned
	tui_send Space
	tui_wait '[x] demo-paused' 10
	tui_wait '[x] demo-pinned' 10
	tui_send u
	tui_wait 'Update 2 stacks?' 15
	tui_wait '  demo-pinned' 15
	# The question offers yes only once the changelogs of both stacks are read; an earlier y is ignored.
	tui_wait 'y yes, do it' 60
	log 'scenario: update question of two stacks:'
	tui_excerpt 'Update 2 stacks' 14
	[ "$(stack_images demo-paused)" = "$image:1.0.0" ] && [ "$(stack_images demo-pinned)" = "$image:1.0.0" ] ||
		fail 'a stack changed before the answer'
	tui_send y
	tui_wait 'update finished: 2 ok, 0 failed' 180
	log 'scenario: result of the two-stack update:'
	tui_excerpt 'update finished' 8
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	[ "$(as_admin "cd ~/stacks && git rev-list --count $head_before..HEAD")" = 2 ] || fail 'the multi-update did not create exactly two commits'
	first_commit=$(as_admin "cd ~/stacks && git log --reverse --format=%H $head_before..HEAD" | sed -n 1p)
	second_commit=$(as_admin "cd ~/stacks && git log --reverse --format=%H $head_before..HEAD" | sed -n 2p)
	tui_commit_only_version "$first_commit" demo-paused 1.0.0 1.1.0
	tui_commit_only_version "$second_commit" demo-pinned 1.0.0 1.1.0
	for stack in demo-paused demo-pinned; do
		[ "$(stack_images "$stack")" = "$image:1.1.0" ] || fail "$stack does not run 1.1.0: $(stack_images "$stack")"
		stack_healthy "$stack" || fail "$stack is not healthy after the update"
	done
	[ "$(stack_field demo-paused deployed)" = "$first_commit" ] && [ "$(stack_field demo-pinned deployed)" = "$second_commit" ] ||
		fail 'the stacks are not recorded as deployed at their update commits'
	first_log=$(qd stack log "$(newest_run demo-paused)") second_log=$(qd stack log "$(newest_run demo-pinned)")
	first_end=$(printf '%s\n' "$first_log" | tail -n 1 | cut -d ' ' -f 1) second_start=$(printf '%s\n' "$second_log" | head -n 1 | cut -d ' ' -f 1)
	[ "$(printf '%s\n%s\n' "$first_end" "$second_start" | sort | head -n 1)" = "$first_end" ] ||
		fail "the second run started before the first ended: $first_end, $second_start"
	log "scenario: two stacks updated one after another: $first_commit, $second_commit"

	step 'X asks first and stops demo-cache; status, stack list and the interface show it as stopped'
	# The summary of the update above is still open and takes every key.
	tui_send Escape
	tui_send 1
	tui_wait '[ ] demo-cache' 10
	tui_send Home
	tui_cursor_to demo-cache
	tui_send X
	tui_wait 'Stop demo-cache and keep it stopped until it is started?' 15
	log 'scenario: stop question of demo-cache:'
	tui_excerpt 'Stop demo-cache' 12
	[ "$(running_containers demo-cache)" -gt 0 ] || fail 'the question already stopped demo-cache'
	[ -z "$(stack_field demo-cache stopped)" ] || fail 'the question already marked demo-cache as stopped'
	tui_send y
	tui_wait 'stop finished: 1 ok, 0 failed' 120
	[ "$(running_containers demo-cache)" -eq 0 ] || fail 'demo-cache still runs after the stop in the interface'
	[ -n "$(stack_field demo-cache stopped)" ] || fail "status does not show demo-cache as stopped: $(qd --output json status)"
	qd stack list | grep demo-cache | grep -q 'stopped since' || fail "stack list does not show demo-cache as stopped: $(qd stack list)"
	tui_send Escape
	i=0
	until tui_screen | grep -qE 'demo-cache .* stopped '; do
		i=$((i + 1))
		[ "$i" -lt 30 ] || fail "the Stacks area does not show demo-cache as stopped: $(tui_screen)"
		tui_send r
		sleep 0.3
	done
	log 'scenario: the Stacks area shows demo-cache as stopped:'
	tui_excerpt 'STACK' 8

	step 'a new commit leaves the stopped demo-cache stopped'
	count=$(stack_run_count demo-cache)
	as_admin 'cd ~/stacks && git pull -q --ff-only &&
		printf "# stopped-stack marker\n" >>demo-cache/.env &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -am "Touch demo-cache while it is stopped in the interface" &&
		git push -q origin main'
	pushed=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	wait_for 'the poll after the restart' 60 poll_done
	wait_for 'the pending commit of demo-cache' 60 field_is demo-cache pending "$pushed"
	[ "$(stack_run_count demo-cache)" = "$count" ] || fail 'a run of the stopped demo-cache was started by the poll'
	[ "$(running_containers demo-cache)" -eq 0 ] || fail 'demo-cache runs again after a new commit'
	[ -n "$(stack_field demo-cache stopped)" ] || fail 'the stopped mark did not survive the service restart'
	tui_send r
	tui_send Home
	tui_cursor_to demo-cache
	tui_screen | grep -qE 'demo-cache .* stopped ' || fail "the Stacks area does not show demo-cache as stopped after the commit: $(tui_screen)"

	step 'S asks first and starts demo-cache; the mark is gone and the interface no longer shows it as stopped'
	tui_send S
	tui_wait 'Start demo-cache?' 15
	[ -n "$(stack_field demo-cache stopped)" ] || fail 'the question already lifted the stopped mark'
	tui_send y
	tui_wait 'start finished: 1 ok, 0 failed' 120
	[ -z "$(stack_field demo-cache stopped)" ] || fail "status still shows demo-cache as stopped: $(qd --output json status)"
	[ "$(running_containers demo-cache)" -gt 0 ] || fail 'demo-cache does not run after the start in the interface'
	stack_healthy demo-cache || fail 'demo-cache is not healthy after the start'
	tui_send Escape
	tui_send r
	i=0
	while tui_screen | grep -qE 'demo-cache .* stopped '; do
		i=$((i + 1))
		[ "$i" -lt 30 ] || fail "the Stacks area still shows demo-cache as stopped: $(tui_screen)"
		tui_send r
		sleep 0.3
	done
	log "scenario: demo-cache stayed stopped against commit $pushed until S started it"

	step 'the Containers area shows a container that is stopped in the background without a key press'
	single=$prefix-single-tui
	docker exec "$host" docker rm -f "$single" >/dev/null 2>&1 || true
	docker exec "$host" docker run -d --name "$single" --label "$label" "$image:1.0.0" >/dev/null ||
		fail 'cannot start the single container for the Containers area'
	wait_for 'the single container' 30 health_is "$single" healthy
	tui_send Escape
	tui_send 5
	tui_wait '5 Containers' 10
	tui_wait 'single containers' 15
	running_seen=0 stopped_seen=0
	tui_screen | grep -qE " $single +ok " && running_seen=1
	docker exec "$host" docker stop "$single" >/dev/null || stopped_seen=failed
	# No key is sent from here on: the area reads the engine again every 5 seconds on its own.
	i=0
	while [ "$stopped_seen" = 0 ] && [ "$i" -lt 20 ]; do
		tui_screen | grep -qE " $single +stopped " && stopped_seen=1
		i=$((i + 1))
		[ "$stopped_seen" = 1 ] || sleep 1
	done
	final_screen=$(tui_screen)
	docker exec "$host" docker rm -f "$single" >/dev/null 2>&1
	[ "$running_seen" = 1 ] || fail "the Containers area does not show $single as ok before the stop: $final_screen"
	[ "$stopped_seen" = 1 ] || fail "the Containers area did not show $single as stopped within 20 seconds without a key press: $final_screen"
	log "scenario: the Containers area followed $single from ok to stopped without a key press"

	step 'the Containers area opens the log of a container, takes a period and a search, and the stack detail opens the log of its stack, both without the secret'
	log_secret=not-a-real-secret-e2e
	ctname=$(docker exec "$host" docker ps --filter label=com.docker.compose.project=demo-stack --format '{{.Names}}' | head -n 1)
	[ -n "$ctname" ] || fail 'no container of demo-stack runs for the log of the interface'
	# A container that runs for minutes has scrolled its start line out of the view; a restart writes a new one.
	docker exec "$host" docker restart "$ctname" >/dev/null || fail 'cannot restart the container for the log of the interface'
	wait_for 'demo-stack healthy' 60 stack_healthy demo-stack
	tui_send Escape
	tui_send 5
	tui_wait 'own stack demo-stack' 15
	tui_send Home
	i=0
	until tui_screen | grep -q "^> .*$ctname "; do
		i=$((i + 1))
		[ "$i" -lt 30 ] || fail "the cursor of the Containers area does not reach $ctname: $(tui_screen)"
		tui_send Down
		sleep 0.2
	done
	tui_send o
	tui_wait 'demo tick' 10
	# The log opens following at its end, so an older container shows its start line, the only one with the
	# secret, only after Home. Home reaches it while it lies within the 500 lines read first (a container up to
	# about 16 minutes old), which covers one scenario run.
	tui_send Home
	tui_wait 'demo started' 10
	tui_screen | grep -qF 'token=[redacted]' || fail "the start line in the log of $ctname shows no masked token: $(tui_screen)"
	tui_has "$log_secret" && fail "the log of $ctname shows the secret: $(tui_screen)"
	tui_send End
	# The demo app writes "demo tick" every 2 seconds: a line that comes in while the log is open was followed.
	tui_wait 'demo tick' 10
	tui_send T Right Tab BSpace BSpace BSpace
	tui_type 1
	tui_send Tab Right F2
	sleep 1
	tui_wait 'demo tick' 10
	tui_screen | grep -qE 'T (period: )?all' && fail "the log still shows no period after the choice: $(tui_screen)"
	tui_send /
	tui_type 'demo tick'
	tui_send Enter
	sleep 0.5
	tui_wait 'demo tick' 10
	tui_send Escape
	tui_send Escape
	tui_wait 'own stack demo-stack' 10
	tui_has 'demo tick' && fail "the log is still open after Escape: $(tui_screen)"
	tui_send Escape
	tui_wait '1 Stacks' 10
	tui_send Home
	tui_cursor_to demo-stack
	tui_send Enter
	tui_send o
	tui_wait 'demo tick' 10
	tui_send Home
	tui_wait 'demo started' 10
	tui_screen | grep -q "$ctname .*demo started" || fail "the stack log does not name the container: $(tui_screen)"
	tui_screen | grep -qF 'token=[redacted]' || fail "the start line in the log of demo-stack shows no masked token: $(tui_screen)"
	tui_has "$log_secret" && fail "the log of demo-stack shows the secret: $(tui_screen)"
	tui_send Escape
	tui_send Escape
	log "scenario: the Containers area and the stack detail opened the log of $ctname and of demo-stack, with a period and a search; the secret stayed masked"

	step 'the Containers area stops, starts and restarts a foreign container after a confirmation and shows each new state without a further key press'
	foreign=$prefix-foreign-tui
	docker exec "$host" docker rm -f "$foreign" >/dev/null 2>&1 || true
	docker exec "$host" docker run -d --name "$foreign" --label "$label" \
		--label com.docker.compose.project=demo-foreign-tui "$image:1.0.0" >/dev/null ||
		fail 'cannot start the foreign container for the container actions'
	wait_for 'the foreign container' 30 health_is "$foreign" healthy
	tui_send Escape
	tui_send 5
	tui_wait '5 Containers' 10
	tui_wait 'foreign project demo-foreign-tui' 15
	# ct_action presses the action key $1 on the row of $foreign (the group order depends on the language, so the
	# cursor is found by name), confirms the question that starts with $2 and closes the result sheet of the
	# command $3. The dialog wraps the question, so only its first words are matched whole.
	ct_action() {
		tui_send Home
		i=0
		until tui_screen | grep -q "^> .*$foreign "; do
			i=$((i + 1))
			[ "$i" -lt 40 ] || fail "the cursor of the Containers area does not reach $foreign: $(tui_screen)"
			tui_send Down
			sleep 0.2
		done
		tui_send "$1"
		tui_wait "$2 container $foreign" 15
		tui_has 'demo-foreign-tui)?' || fail "the question of $3 does not name the foreign project: $(tui_screen)"
		tui_excerpt "$2 container $foreign" 10
		tui_send y
		tui_wait "$3 finished: 1 ok, 0 failed" 60
		tui_has "$foreign" || fail "the result sheet of $3 does not name $foreign: $(tui_screen)"
		tui_send Escape
		tui_wait '5 Containers' 10
	}
	# ct_expect waits without a key press for the row of $foreign to show the state $1 and checks the engine state $2.
	ct_expect() {
		i=0
		until tui_screen | grep -qE " $foreign +$1 "; do
			i=$((i + 1))
			[ "$i" -lt 20 ] || fail "the Containers area does not show $foreign as $1 after the action: $(tui_screen)"
			sleep 1
		done
		engine=$(docker exec "$host" docker inspect -f '{{.State.Status}}' "$foreign")
		[ "$engine" = "$2" ] || fail "docker shows $foreign as $engine, not $2: $(tui_screen)"
	}
	tui_screen | grep -qE " $foreign +ok " || fail "the Containers area does not show $foreign as ok before the stop: $(tui_screen)"

	ct_action X Stop stop
	ct_expect stopped exited
	log "scenario: the stop through the Containers area left $foreign stopped in the list and exited in docker"

	ct_action S Start start
	ct_expect ok running
	log "scenario: the start through the Containers area left $foreign ok in the list and running in docker"

	started=$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' "$foreign")
	ct_action Z Restart restart
	ct_expect ok running
	[ "$(docker exec "$host" docker inspect -f '{{.State.StartedAt}}' "$foreign")" != "$started" ] ||
		fail "the restart did not restart $foreign: $(tui_screen)"
	docker exec "$host" docker rm -f "$foreign" >/dev/null 2>&1
	log "scenario: the restart through the Containers area left $foreign ok in the list and running in docker with a new start time"

	step 'Tab focuses the tab line, Right and Left switch the area without digits, and Tab leads back to the content'
	tui_send Escape
	# An earlier step left a run log open in Runs; close it so Runs shows its list.
	tui_send 3
	tui_send Escape
	tui_wait 'STARTED' 10
	tui_send 1
	tui_wait '1 Stacks' 10
	# The tab line has the focus while the footer offers "next area"; the area shows in its content: the
	# Stacks list has the CURRENT column, Stack detail a "mode" row (or the hint that no stack is chosen),
	# Runs the STARTED column.
	tui_send Tab
	tui_wait 'next area' 10
	tui_has 'CURRENT' || fail "the Stacks area is not shown with the focus on the tab line: $(tui_screen)"
	tui_send Right
	i=0
	until tui_screen | grep -qE '^ +(mode +|No stack chosen)'; do
		i=$((i + 1))
		[ "$i" -lt 40 ] || fail "Right did not show the Stack detail area: $(tui_screen)"
		sleep 0.25
	done
	tui_send Right
	tui_wait 'STARTED' 10
	tui_send Left
	i=0
	until tui_screen | grep -qE '^ +(mode +|No stack chosen)'; do
		i=$((i + 1))
		[ "$i" -lt 40 ] || fail "Left did not lead back to the Stack detail area: $(tui_screen)"
		sleep 0.25
	done
	tui_has 'next area' || fail "the tab line lost the focus while switching: $(tui_screen)"
	tui_send Tab
	sleep 0.5
	tui_has 'next area' && fail "Tab does not lead back to the content: $(tui_screen)"
	tui_screen | grep -qE '^ +(mode +|No stack chosen)' || fail "the area changed with Tab: $(tui_screen)"
	log 'scenario: Tab, Right and Left switched the area without digits and Tab led back to the content'

	step 'q ends the interface with exit code 0'
	tui_send Escape
	tui_send 1
	sleep 0.3
	tui_send q
	tui_wait 'qdeploy-exit=0' 10
	tui_tmux kill-server >/dev/null 2>&1 || true
}

# tui_commit_only_version requires that a commit moves only DEMO_APP_VERSION of one stack, from $3 to $4, with
# the subject the update gives it.
tui_commit_only_version() {
	[ "$(as_admin "cd ~/stacks && git show --format= --numstat $1")" = "$(printf '1\t1\t%s/.env' "$2")" ] ||
		fail "the commit $1 does not change exactly one line of $2/.env"
	[ "$(as_admin "cd ~/stacks && git show --format= -U0 $1 | grep '^[-+]' | grep -v '^\(---\|+++\)'")" = "$(printf -- '-DEMO_APP_VERSION=%s\n+DEMO_APP_VERSION=%s' "$3" "$4")" ] ||
		fail "the commit $1 does not only move DEMO_APP_VERSION of $2 from $3 to $4"
	[ "$(as_admin "cd ~/stacks && git log -1 --format=%s $1")" = "Update $2 DEMO_APP_VERSION from $3 to $4" ] ||
		fail "the commit $1 has an unexpected subject: $(as_admin "cd ~/stacks && git log -1 --format=%s $1")"
}

health_is() {
	[ "$(docker exec "$host" docker inspect -f '{{.State.Health.Status}}' "$1")" = "$2" ]
}

# --- First setup of a fresh service through the terminal interface only ---

tui_login_user=alice-e2e
tui_registry_host=quay.other.example.test
tui_login_value=tui-registry-login-e2e
tui_hook_url_value=https://hooks.$domain/tui-hook-e2e-path
tui_hook_signing_value=tui-signing-value-e2e
tui_stack_value=tuivalue-e2e-0451

# tui_type types text into the interface.
tui_type() {
	tui_tmux send-keys -t qdtui -l -- "$1"
}

# tui_clear empties the text field that has the focus.
tui_clear() {
	tui_send BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace
	tui_send BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace BSpace
}

# tui_menu shows the menu of the Settings area, whatever page was open.
tui_menu() {
	tui_send 4
	sleep 0.4
	tui_has 'bind the repository' || {
		tui_send Escape
		sleep 0.4
	}
	tui_wait 'bind the repository' 10
}

# tui_page opens the page of the Settings menu that is $1 rows below the first.
tui_page() {
	tui_menu
	tui_send Home
	n=0
	while [ "$n" -lt "$1" ]; do
		tui_send Down
		n=$((n + 1))
	done
	tui_send Enter
}

# tui_pick_to moves the cursor of an open choice to the row that starts with the text $1.
tui_pick_to() {
	i=0
	until tui_screen | grep -q "^> *$1"; do
		i=$((i + 1))
		[ "$i" -lt 12 ] || fail "the cursor does not reach $1: $(tui_screen)"
		tui_send Down
		sleep 0.2
	done
}

# first_setup_reset gives the service a fresh state: no key, credentials, channels, repository or runs, and
# no containers of the earlier sections.
first_setup_reset() {
	docker exec "$host" systemctl stop qdeploy || fail 'cannot stop the qdeploy service'
	docker exec "$host" sh -ec '
		for c in $(docker ps -aq --filter label=com.docker.compose.project); do docker rm -f -v "$c" >/dev/null; done
		for v in $(docker volume ls -q --filter label=com.docker.compose.project); do docker volume rm "$v" >/dev/null; done
		find /opt/stacks /opt/stacks2 /opt/stacks3 /var/lib/qdeploy -mindepth 1 -delete' || fail 'cannot reset the state of the service'
	docker exec "$host" systemctl start qdeploy ||
		fail 'cannot start the fresh qdeploy service'
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	wait_for 'the fresh qdeploy service to answer' 20 qd status
}

# first_setup_stack adds the stack demo-echo to the repository: its container name carries the value of the
# secret DEMO_ECHO_TAG, so Compose prints that value when it creates and starts the container.
first_setup_stack() {
	as_admin 'cd ~/stacks && git pull -q --ff-only && mkdir demo-echo'
	cat <<EOF | as_admin 'cat >"$HOME"/stacks/demo-echo/compose.yaml'
services:
  app:
    image: registry.$domain/demo/app:\${DEMO_APP_VERSION}
    container_name: demo-echo-\${DEMO_ECHO_TAG}
    restart: unless-stopped
    environment:
      DEMO_NAME: demo-echo
      DEMO_VERSION: \${DEMO_APP_VERSION}
      DEMO_HEALTH: \${DEMO_HEALTH}
    volumes:
      - ./data:/data
EOF
	printf 'version: 1\nbackup:\n  paths:\n    - data\n  retention:\n    keep: 5\n' | as_admin 'cat >"$HOME"/stacks/.qatlas/qdeploy/demo-echo.yaml'
	printf 'DEMO_APP_VERSION=1.0.1\nDEMO_HEALTH=ok\n' | as_admin 'cat >"$HOME"/stacks/demo-echo/.env'
	as_admin 'cd ~/stacks && cp demo-cache/.gitignore demo-echo/.gitignore && git add -A &&
		git -c user.name="E2E Admin" -c user.email=admin.qdeploye2e.test commit -q -m "Add the demo-echo stack" && git push -q origin main'
}

scenario_tui_setup() {
	tui_log=$work/tui-screens.log
	: >"$tui_log"
	image=registry.$domain/demo/app

	step 'the service gets a fresh state; the stack demo-echo joins the repository'
	first_setup_stack
	first_setup_reset
	if qd key show >/dev/null; then fail 'the fresh service has a deploy key'; fi
	if qd credential list | grep -q registry; then fail "the fresh service has credentials: $(qd credential list)"; fi

	step 'sudo qdeploy opens the interface of the fresh service'
	tui_tmux kill-server >/dev/null 2>&1 || true
	tui_tmux new-session -d -s qdtui -x 120 -y 40 "env LANG=C.UTF-8 sh -c 'sudo qdeploy; echo qdeploy-exit=\$?; sleep 600'" ||
		fail 'cannot start tmux for the interface'
	tui_wait '1 Stacks' 30
	tui_wait 'No stacks' 30

	step 'Settings: the deploy key is generated and its public part shown'
	tui_page 1
	tui_wait 'No deploy key yet' 10
	tui_send G
	tui_wait 'Generate the deploy key?' 10
	tui_send y
	tui_wait 'The deploy key is generated' 30
	tui_send Enter
	tui_wait 'Add this public key' 10
	pub=$(tui_screen | grep -o 'ssh-ed25519 [A-Za-z0-9+/=]*' | head -n 1)
	[ -n "$pub" ] || fail "the public key is not on the screen: $(tui_screen)"
	cli_pub=$(qd --output json key show | sed -n 's/.*"public_key": *"\([^"]*\)".*/\1/p' | cut -d ' ' -f 1,2)
	[ "$pub" = "$cli_pub" ] || fail "the key on the screen differs from the key of the service: $pub vs $cli_pub"
	printf '%s\n' "$pub" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	log 'scenario: deploy key generated through the interface and registered on the git server'

	step 'Settings: a registry credential is set after a question'
	tui_page 2
	tui_wait 'Credentials' 10
	tui_send a
	tui_wait 'Set a credential' 10
	tui_send Tab
	tui_type "$tui_registry_host"
	# Escape on a changed form asks first; the default answer keeps the form and its value.
	tui_send Escape
	tui_wait 'Discard the changes to this form?' 10
	tui_send Enter
	tui_wait "$tui_registry_host" 10
	tui_has 'Set a credential' || fail "cancelling the discard question closed the form: $(tui_screen)"
	# A click takes the focus of a field without typing, and a click on the Save button saves like F2.
	tui_click 'Registry user'
	sleep 0.3
	tui_type "$tui_login_user"
	tui_send Tab
	tui_type "$tui_login_value"
	sleep 0.5
	tui_click '[ F2 save ]'
	tui_wait 'Store the credential' 10
	tui_send y
	tui_wait 'is stored' 20
	tui_send Enter
	tui_wait "registry.$tui_registry_host" 10
	qd credential list | grep -q "registry.$tui_registry_host" || fail "the credential is not stored: $(qd credential list)"

	step 'Settings: the maintenance window is moved away from now'
	win_start=$(date -u -d '+12 hours' +%H:%M)
	tui_page 3
	tui_wait 'Maintenance window' 10
	tui_send e
	tui_wait 'Change the maintenance window' 10
	tui_clear
	tui_type daily
	tui_send Tab
	tui_clear
	tui_type "$win_start"
	tui_send Tab
	tui_clear
	tui_type 1m
	tui_send Tab
	tui_clear
	tui_type UTC
	tui_send F2
	tui_wait 'Change the maintenance window?' 10
	tui_send y
	tui_wait 'The maintenance window is changed' 20
	tui_send Enter
	[ "$(window_field start)" = "$win_start" ] && [ "$(window_field open)" = false ] ||
		fail "the window was not set through the interface: $(qd --output json window show)"

	step 'Settings: a webhook channel is added and its test message is sent'
	tui_page 4
	tui_wait 'Notifications' 10
	tui_send a
	tui_wait 'Add a notification channel' 10
	tui_type tui-hook
	tui_send F2
	tui_wait 'Settings of the channel' 10
	tui_type "$tui_hook_url_value"
	tui_send Tab
	tui_type "$tui_hook_signing_value"
	tui_send F2
	tui_wait 'Add the channel tui-hook' 10
	tui_send y
	tui_wait 'The channel tui-hook is added' 20
	tui_send Enter
	tui_wait 'tui-hook' 10
	tui_send s
	tui_wait 'Send a test message through tui-hook?' 10
	tui_send y
	tui_wait 'was delivered' 60
	tui_send Enter
	hook_seen /tui-hook-e2e-path test || fail 'the webhook receiver did not record the test message of the interface'

	step 'Settings: the repository is bound after the host key is shown and confirmed'
	tui_page 0
	tui_wait 'No stacks repository is bound' 10
	tui_send b
	tui_wait 'Bind the stacks repository' 10
	tui_type "git.$domain:stacks.git"
	tui_send Tab
	tui_type /opt/stacks
	tui_send F2
	expected=$(git_ed25519_fingerprint)
	tui_wait "$(printf '%s' "$expected" | cut -c 1-30)" 60
	tui_wait 'ssh-ed25519' 10
	before=$(known_hosts_sum)
	tui_send y
	tui_wait 'The stacks repository is bound' 120
	tui_send Enter
	[ "$(known_hosts_sum)" != "$before" ] || fail 'no host key was pinned'
	docker exec "$host" test -f /opt/stacks/demo-echo/compose.yaml || fail 'the clone is missing in /opt/stacks'
	log "scenario: repository bound through the interface, host key $expected confirmed"

	step 'the Stacks area lists the stacks of the new repository'
	tui_send 1
	tui_wait 'demo-echo' 60
	wait_for 'the first run of demo-echo' 180 stack_idle demo-echo

	step 'Stack detail: a secret is set after a question and the file holds the value'
	tui_cursor_to demo-echo
	tui_send Enter
	tui_wait 'Secrets' 15
	tui_send a
	tui_wait 'Set a secret' 10
	tui_type DEMO_ECHO_TAG
	tui_send Tab
	tui_type "$tui_stack_value"
	sleep 0.5
	tui_send F2
	tui_wait 'the secret DEMO_ECHO_TAG' 10
	if docker exec "$host" test -e /opt/stacks/demo-echo/secrets.env; then fail 'the secret was stored before the answer'; fi
	tui_send y
	tui_wait 'set for the stack demo-echo' 20
	tui_send Enter
	tui_wait 'DEMO_ECHO_TAG' 10
	[ "$(docker exec "$host" cat /opt/stacks/demo-echo/secrets.env)" = "DEMO_ECHO_TAG='$tui_stack_value'" ] ||
		fail "secrets.env does not hold the secret: $(docker exec "$host" cat /opt/stacks/demo-echo/secrets.env)"
	[ "$(docker exec "$host" stat -c %U:%a /opt/stacks/demo-echo/secrets.env)" = qdeploy:600 ] || fail 'secrets.env has the wrong owner or mode'
	[ -z "$(docker exec -u qdeploy "$host" git -C /opt/stacks status --porcelain)" ] || fail 'the secret made the working copy dirty'

	step 'Stacks: the first deploy of demo-echo succeeds; Compose prints the value and the run log hides it'
	prev=$(newest_run demo-echo)
	tui_send Escape
	tui_wait 'CURRENT' 10
	tui_cursor_to demo-echo
	tui_send d
	tui_wait 'Deploy demo-echo?' 10
	tui_send y
	tui_wait 'deploy finished: 1 ok, 0 failed' 60
	tui_send l
	tui_wait 'result succeeded' 240
	run=$(next_run demo-echo "$prev") || fail 'no run of demo-echo'
	wait_for "the run $run" 240 run_finished demo-echo "$run"
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result succeeded' || fail "the deploy of demo-echo did not succeed: $run_log"
	[ "$(stack_images demo-echo)" = "$image:1.0.1" ] || fail "demo-echo does not run 1.0.1: $(stack_images demo-echo)"
	docker exec "$host" docker inspect -f '{{.Name}}' "demo-echo-$tui_stack_value" >/dev/null || fail 'the container name does not carry the value'
	printf '%s' "$run_log" | grep -q 'demo-echo-\[redacted\]' || fail "the run log does not show what Compose printed with the value redacted: $run_log"
	if printf '%s' "$run_log" | grep -qF -- "$tui_stack_value"; then fail 'the run log holds the value of the secret'; fi
	log 'scenario: first deploy of demo-echo through the interface; Compose printed the value and the run log redacted it'
	wait_for 'the webhook for the run of demo-echo' 60 hook_seen /tui-hook-e2e-path succeeded demo-echo

	step 'Stack detail: an older backup is restored after a question'
	docker exec "$host" sh -ec 'mkdir -p /opt/stacks/demo-echo/data && printf first >/opt/stacks/demo-echo/data/marker.txt'
	out=$(qd --output json stack backup create demo-echo) || fail "backup create failed: $out"
	older=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	# Backup ids start with the second of their creation; two in one second would not be ordered by age.
	sleep 2
	docker exec "$host" sh -ec 'printf second >/opt/stacks/demo-echo/data/marker.txt'
	out=$(qd --output json stack backup create demo-echo) || fail "backup create failed: $out"
	newest=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	[ -n "$older" ] && [ "$older" != "$newest" ] || fail "no two backups: $older, $newest"
	tui_send 1
	tui_wait 'CURRENT' 10
	tui_cursor_to demo-echo
	tui_send Enter
	tui_wait 'Backups' 15
	tui_send r
	tui_wait "$newest" 15
	tui_send R
	tui_wait 'Restore a backup' 10
	if tui_screen | grep -q "$newest"; then fail 'the newest backup is offered for a restore'; fi
	tui_pick_to "$older"
	tui_send Enter
	tui_wait 'Restore the backup' 10
	tui_wait 'overwritten' 10
	[ "$(docker exec "$host" cat /opt/stacks/demo-echo/data/marker.txt)" = second ] || fail 'the question already changed the data'
	tui_send y
	tui_wait 'restored for the stack' 600
	tui_send Enter
	[ "$(docker exec "$host" cat /opt/stacks/demo-echo/data/marker.txt)" = first ] ||
		fail "the restore did not bring back the data: $(docker exec "$host" cat /opt/stacks/demo-echo/data/marker.txt)"
	wait_for 'demo-echo to run again after the restore' 120 stack_healthy demo-echo
	log "scenario: the older backup $older of demo-echo was restored through the interface"

	step 'q ends the interface with exit code 0'
	tui_send Escape
	tui_send 1
	sleep 0.3
	tui_send q
	tui_wait 'qdeploy-exit=0' 10
	tui_tmux kill-server >/dev/null 2>&1 || true

	step 'no masked value shows on a screen, in a run log, in the journal, in a listing or in a file but secrets.env'
	[ "$(wc -l <"$tui_log")" -gt 200 ] || fail 'too few screens were captured to check'
	grep -qF DEMO_ECHO_TAG "$tui_log" || fail 'the captured screens do not show the key of the secret'
	runs_text=$(qd stack runs)
	for id in $(qd stack runs | grep -o '[0-9]\{8\}T[0-9]\{6\}Z-[0-9a-f]\{8\}'); do
		runs_text="$runs_text
$(qd stack log "$id")"
	done
	listing=$(qd notify list; qd --output json notify list; qd credential list; qd --output json credential list; qd --output json status)
	journal_text=$(journal)
	screens_text=$(cat "$tui_log")
	for value in "$tui_login_value" "$tui_hook_url_value" tui-hook-e2e-path "$tui_hook_signing_value" "$tui_stack_value"; do
		for place in screens_text runs_text journal_text listing; do
			eval "text=\$$place"
			if printf '%s' "$text" | grep -qF -- "$value"; then fail "a masked value shows in $place"; fi
		done
	done
	if docker exec "$prefix-hooks" cat /data/requests.jsonl | grep -qF -- "$tui_stack_value"; then
		fail 'the value of the stack secret reached the webhook receiver'
	fi
	holders=$(docker exec "$host" grep -rlF -- "$tui_stack_value" /opt/stacks /var/lib/qdeploy 2>/dev/null || true)
	[ "$holders" = /opt/stacks/demo-echo/secrets.env ] || fail "the value of the stack secret stands in: $holders"
	log 'scenario: no masked value on a captured screen, in a run log, the journal or a listing; the secret value only in secrets.env'
}
