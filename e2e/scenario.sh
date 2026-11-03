#!/bin/sh
# Scenario: from the simulated host, clone the demo stacks repository over SSH and push a commit; then check
# the demo image tags and that a demo container turns unhealthy on purpose. Then run qdeploy on the host as a
# service, bind the stacks repository with its own deploy key, list the stacks, deploy a new commit, show the
# available image versions per update ceiling without changing anything, roll back a
# commit with a failing healthcheck (data restored from the run's backup, revert commit on the remote), leave a
# stack whose rollback fails stopped and paused until resume, and refuse a changed host key of the git server.
# Version updates with qdeploy stack update follow: one direct step, a stepwise major jump and a jump whose middle step
# fails. The maintenance window follows on a real, short window: a stack in automatic mode is updated without a
# human, a stack in manual mode is only reported, a paused and a pinned stack stay as they are, and a change with
# window set takes effect in the running service. The terminal interface follows: sudo qdeploy opens it in a
# tmux pane, an update shows its changelog before it is confirmed and then runs, and two ticked stacks are updated
# one after another with one confirmation. Docker integration tests of the deployer run inside the host
# before all that. Sourced by run.sh scenario.

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
		useradd --system --home-dir /var/lib/qdeploy --no-create-home --shell /usr/sbin/nologin --groups docker qdeploy
		install -d -o qdeploy -g qdeploy -m 0755 /opt/stacks
		systemd-run --quiet --unit=qdeploy-e2e --property=Type=exec --property=User=qdeploy --property=Group=qdeploy \
			--property=StateDirectory=qdeploy --property=StateDirectoryMode=0700 \
			--property=RuntimeDirectory=qdeploy --property=RuntimeDirectoryMode=0750 /usr/local/bin/qdeploy run --backup-image '"$backup_image"'' ||
		fail 'cannot start the qdeploy service'
	wait_for 'the qdeploy service channel' 20 docker exec "$host" sh -c 'ls /run/qdeploy | grep -q sock'
	log 'scenario: qdeploy service running'
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
	for name in TestDockerHealthyStackIsDeployedAndChecked TestDockerUnhealthyStackFailsTheCheck TestDockerMissingSecretsFileStopsBeforeCompose; do
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
	docker exec "$host" systemctl restart qdeploy-e2e || fail 'cannot restart the qdeploy service'
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

scenario_deploy() {
	secret=not-a-real-secret-e2e

	step 'the runs started by repo set end; secrets.env is not there yet'
	wait_for 'the first run of demo-stack' 120 stack_idle demo-stack
	wait_for 'the first run of demo-cache' 120 stack_idle demo-cache
	log "scenario: first runs: demo-stack $(stack_field demo-stack last_run), demo-cache $(stack_field demo-cache last_run)"

	step 'secrets.env is created on the host, owned by the service user'
	for stack in demo-stack demo-cache; do
		docker exec -u qdeploy "$host" sh -ec "umask 077 && printf 'DEMO_SECRET=%s\n' '$secret' >/opt/stacks/$stack/secrets.env"
	done
	dirty=$(docker exec -u qdeploy "$host" git -C /opt/stacks status --porcelain 2>&1) || fail "git status failed: $dirty"
	[ -z "$dirty" ] || fail "secrets.env made the working copy dirty: $dirty"
	[ "$(docker exec "$host" stat -c %U:%a /opt/stacks/demo-stack/secrets.env)" = qdeploy:600 ] || fail 'secrets.env has the wrong owner or mode'

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

	step 'the engine deploys a new commit it finds by polling'
	good=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	restart_service
	wait_for 'the run of demo-cache' 240 stack_settled demo-cache succeeded
	[ "$(stack_field demo-cache deployed)" = "$good" ] || fail "demo-cache is not recorded as deployed at $good: $(qd --output json status)"
	[ "$(stack_images demo-cache)" = "registry.$domain/demo/app:1.1.0" ] || fail "demo-cache does not run the image 1.1.0: $(stack_images demo-cache)"
	log "scenario: poll deployed demo-cache at $good"

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
	[ "$n" -ge 3 ] || fail "too few runs to check the logs: $n"
	if qd stack runs | grep -qF -- "$secret"; then fail 'qdeploy stack runs shows the secret value'; fi
	log "scenario: the secret value is in none of $n run logs"
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
	out=$(qd_fails stack backup create demo-badpath) || fail "backup create accepted a path outside the stack: $out"
	printf '%s' "$out" | grep -q -- 'config-invalid' || fail "backup create does not refuse the invalid rules: $out"
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

# updates_commit changes the rules of demo-stack and waits for the run the new commit starts.
updates_commit() {
	prev=$(newest_run demo-stack)
	admin_commit demo-stack/qdeploy.yaml "$1" "$2"
	restart_service
	run=$(next_run demo-stack "$prev") || fail "no run of demo-stack started for: $2"
	wait_for "the run $run" 240 run_finished demo-stack "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run for '$2' did not succeed: $(qd stack log "$run")"
}

# scenario_updates shows the update detection against the registry: the stored result, the ceiling of the stack,
# the major hint and a stack without rules. Detection changes nothing; only the admin's commits do.
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

	step 'a stack without rules is listed too'
	out=$(qd --output json stack updates demo-cache --refresh) || fail "updates demo-cache failed: $out"
	updates_expect "$out" 1.1.0 1.1.0 5.0.0 current

	step 'ceiling patch offers no minor step; ceiling major offers 5.0.0 without a hint'
	updates_commit 's/^version: 1$/version: 1\nupdate:\n  ceiling: patch/' 'Set the demo-stack update ceiling to patch'
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates failed: $out"
	updates_expect "$out" 1.0.1 1.0.1 5.0.0 current
	updates_commit 's/ceiling: patch/ceiling: major/' 'Set the demo-stack update ceiling to major'
	out=$(qd --output json stack updates demo-stack --refresh) || fail "updates failed: $out"
	updates_expect "$out" 1.0.1 5.0.0 '' update

	step 'the demo-stack rules are restored'
	updates_commit '/^update:$/d; /^  ceiling: major$/d' 'Remove the demo-stack update rules'
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

# update_reset commits a version and a major strategy for demo-stack in one commit and waits for its run.
update_reset() {
	prev=$(newest_run demo-stack)
	as_admin "cd ~/stacks && git pull -q --ff-only &&
		sed -i 's/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=$1/' demo-stack/.env &&
		sed -i 's/^  major: .*/  major: $2/' demo-stack/qdeploy.yaml &&
		git -c user.name='E2E Admin' -c user.email=admin.qdeploye2e.test commit -q -am 'Reset demo-stack to $1 with $2 major updates' &&
		git push -q origin main"
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

	step 'a stack without qdeploy.yaml is not updated'
	out=$(qd_fails stack update demo-cache) || fail "update accepted a stack without rules: $out"
	printf '%s' "$out" | grep -q 'qdeploy.yaml' || fail "the refusal does not name qdeploy.yaml: $out"

	step 'a direct update creates one commit that moves only the image variable, and the stack runs the new version'
	updates_commit 's/^version: 1$/version: 1\nupdate:\n  major: direct/' 'Set the demo-stack major updates to direct'
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

	step 'the demo-stack rules and version are restored'
	update_reset 1.1.0 stepwise
	updates_commit '/^update:$/d; /^  major: stepwise$/d' 'Remove the demo-stack update rules'
	[ "$(stack_images demo-stack)" = "$image:1.1.0" ] || fail "demo-stack does not run 1.1.0 again: $(stack_images demo-stack)"
}

scenario_stacks_repo() {
	setup_service
	url=git.$domain:stacks.git

	# The default window (daily 03:00) must not open in the middle of the other sections on a real clock.
	window_far_away >/dev/null || fail 'cannot move the maintenance window out of the way'

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

	step 'stack list shows the demo stacks with image versions'
	out=$(qd --output json stack list) || fail "stack list failed: $out"
	log "scenario: stack list $out"
	for want in demo-stack demo-cache DEMO_APP_VERSION=1.0.1 DEMO_APP_VERSION=1.0.0; do
		printf '%s' "$out" | grep -q -- "$want" || fail "stack list does not show $want: $out"
	done

	scenario_deploy

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
	docker exec "$host" journalctl -u qdeploy-e2e --no-pager -o cat
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
	as_admin 'cd ~/stacks && git pull -q --ff-only &&
		for s in demo-paused demo-pinned; do
			cp -r demo-cache $s && sed -i "s/demo-cache/$s/" $s/compose.yaml &&
			sed -i "s/^DEMO_APP_VERSION=.*/DEMO_APP_VERSION=1.0.0/" $s/.env &&
			printf "version: 1\nupdate:\n  mode: automatic\n" >$s/qdeploy.yaml
		done &&
		printf "update:\n  mode: automatic\n" >>demo-stack/qdeploy.yaml &&
		printf "version: 1\nupdate:\n  mode: manual\n" >demo-cache/qdeploy.yaml &&
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

tui_screen() {
	tui_tmux capture-pane -p -t qdtui
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
	prev=$(newest_run demo-cache)
	admin_commit demo-cache/qdeploy.yaml 's|^  mode: manual$|  mode: manual\n  changelog:\n    DEMO_APP_VERSION: qdeploye2e-org/demo-image|' 'Add the demo-cache changelog source'
	restart_service
	run=$(next_run demo-cache "$prev") || fail 'no run of demo-cache started for the changelog source'
	wait_for "the run $run" 240 run_finished demo-cache "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run for the changelog source did not succeed: $(qd stack log "$run")"
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
