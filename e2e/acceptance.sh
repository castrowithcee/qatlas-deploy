#!/bin/sh
# Acceptance scenario: a fresh qdeploy installation looks after n8n and BookStack, the way an administrator would
# set it up. The service comes from the signed test release with the installer and backs up with its default helper
# image (no --backup-image option). The first setup goes through the terminal interface only: deploy key, repository
# with a confirmed host key, maintenance window, a webhook channel and the stack secrets from secrets.example.env.
# Both stacks are deployed and get test data. A real maintenance window then updates them with a version commit
# each, after the changelog was shown; a later update to the test tags with a failing healthcheck ends with the
# data restored, a revert commit and a message at the webhook; an older backup is restored with owners and modes
# of the data kept; a stack command must not wait more than 5 seconds during a deliberately slow fetch or poll.
# Sourced by run.sh acceptance after scenario.sh, whose helpers it uses.

# The stack folder is the administrator's free choice; it does not exist until the setup creates it.
acc_stacks=/srv/services/stacks
acc_hook_path=/acceptance-hook-e2e
acc_hook_signing=acceptance-signing-value-e2e
acc_owner_email=owner@acceptance.example.test
acc_owner_password='Acceptance-Pass-1'
acc_jar=/tmp/qdeploye2e-n8n.jar
acc_alpha='Acceptance Alpha'
acc_beta='Acceptance Beta'

acc() {
	log "acceptance: $*"
}

# acc_url prints the address of the HTTP check of a stack on the host.
acc_url() {
	case $1 in
	n8n) printf 'http://127.0.0.1:5678/healthz' ;;
	bookstack) printf 'http://127.0.0.1:6875/status' ;;
	esac
}

# acc_healthy succeeds when all containers of the stack run, none is unhealthy and the HTTP check answers.
acc_healthy() {
	want=1
	[ "$1" != bookstack ] || want=2
	filter="label=com.docker.compose.project=$1"
	[ "$(docker exec "$host" docker ps -q --filter "$filter" --filter status=running | wc -l)" = "$want" ] &&
		[ -z "$(docker exec "$host" docker ps -q --filter "$filter" --filter health=unhealthy --filter health=starting)" ] &&
		[ "$(http_code "$(acc_url "$1")")" = 200 ]
}

# acc_n8n sends a request to n8n on the host with the cookie of the owner session; a third argument is the JSON body.
acc_n8n() {
	if [ $# -ge 3 ]; then
		printf '%s' "$3" | docker exec -i "$host" curl -sS -X "$1" -b "$acc_jar" -c "$acc_jar" \
			-H 'Content-Type: application/json' --data-binary @- "http://127.0.0.1:5678$2"
	else
		docker exec -i "$host" curl -sS -X "$1" -b "$acc_jar" -c "$acc_jar" "http://127.0.0.1:5678$2"
	fi
}

# acc_n8n_login opens the session of the owner; n8n answers /healthz a little before its REST routes are there.
acc_n8n_login() {
	i=0 first=
	while :; do
		out=$(acc_n8n POST /rest/login "{\"emailOrLdapLoginId\":\"$acc_owner_email\",\"password\":\"$acc_owner_password\"}" || true)
		printf '%s' "$out" | grep -qF "$acc_owner_email" && return 0
		[ -n "$first" ] || first=$out
		i=$((i + 1))
		[ "$i" -lt 60 ] || fail "n8n login did not return the owner: $out (first answer: $first)"
		sleep 2
	done
}

# acc_sql runs the SQL text $1 as the database root of BookStack and prints the rows without a header.
acc_sql() {
	cid=$(docker exec "$host" docker ps -q --filter label=com.docker.compose.project=bookstack \
		--filter label=com.docker.compose.service=db)
	[ -n "$cid" ] || fail 'bookstack has no database container'
	printf '%s\n' "$1" | docker exec -i "$host" docker exec -i "$cid" sh -c \
		'exec mariadb -uroot -p"$MARIADB_ROOT_PASSWORD" -N bookstack' || fail "the SQL statement failed: $1"
}

# acc_add creates the test item $2 (a workflow of n8n, a book of BookStack) in the stack $1.
acc_add() {
	case $1 in
	n8n)
		acc_n8n_login
		out=$(acc_n8n POST /rest/workflows "{\"name\":\"$2\",\"nodes\":[],\"connections\":{},\"settings\":{},\"active\":false}") ||
			fail "creating the workflow failed: $out"
		printf '%s' "$out" | grep -qF "\"name\":\"$2\"" || fail "n8n did not create the workflow $2: $out"
		;;
	bookstack)
		slug=$(printf '%s' "$2" | tr 'A-Z ' 'a-z-')
		acc_sql "INSERT INTO entities (type, name, slug, created_at, updated_at, created_by, updated_by, owned_by)
			VALUES ('book', '$2', '$slug', NOW(), NOW(), 1, 1, 1);
			INSERT INTO entity_container_data (entity_id, entity_type, description, description_html)
			VALUES (LAST_INSERT_ID(), 'book', 'invented acceptance data', '<p>invented acceptance data</p>');" >/dev/null
		;;
	esac
}

# acc_names prints the test items of the stack $1 as a comma separated list, sorted.
acc_names() {
	case $1 in
	n8n)
		acc_n8n_login
		acc_n8n GET /rest/workflows | grep -o '"name":"Acceptance [^"]*"' | cut -d '"' -f 4 | sort | tr '\n' , | sed 's/,$//'
		;;
	bookstack)
		acc_sql "SELECT name FROM entities WHERE type = 'book' AND name LIKE 'Acceptance %' ORDER BY name;" | tr '\n' , | sed 's/,$//'
		;;
	esac
}

# acc_expect_items fails unless the stack $1 holds exactly the test items $2.
acc_expect_items() {
	got=$(acc_names "$1")
	[ "$got" = "$2" ] || fail "$1 holds the test items '$got', want '$2'"
}

# acc_tree prints path, owner, group and mode of everything below the data directory of a stack.
acc_tree() {
	docker exec "$host" find "$acc_stacks/$1/data" -printf '%p\t%U:%G:%m\n' | sort
}

# acc_env_has fails unless the stacks repository on the remote holds the line $2 in the .env of the stack $1.
acc_env_has() {
	as_admin "cd ~/stacks && git pull -q --ff-only && grep -qx '$2' $1/.env" || fail "$1/.env has no line $2"
}

# acc_cursor_to moves the cursor of the Stacks list to the stack $1, from wherever it stands.
acc_cursor_to() {
	tui_send Home
	sleep 0.3
	tui_cursor_to "$1"
}

# acc_tui_secret sets the secret $2=$3 of the stack $1 in the detail page of the terminal interface.
acc_tui_secret() {
	tui_send a
	tui_wait 'Set a secret' 10
	tui_type "$2"
	tui_send Tab
	tui_type "$3"
	sleep 0.5
	tui_send F2
	tui_wait "the secret $2" 10
	tui_send y
	tui_wait "set for the stack $1" 20
	tui_send Enter
	tui_wait "$2" 10
}

# acc_deploy_via_tui deploys the stack $1 from the Stacks list and waits for a successful run.
acc_deploy_via_tui() {
	prev=$(newest_run "$1")
	tui_wait 'CURRENT' 10
	acc_cursor_to "$1"
	tui_send d
	tui_wait "Deploy $1?" 10
	tui_send y
	tui_wait 'deploy finished: 1 ok, 0 failed' 600
	tui_send Escape
	run=$(next_run "$1" "$prev") || fail "no run of $1"
	wait_for "the run $run" 600 run_finished "$1" "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the deploy of $1 did not succeed: $(qd stack log "$run")"
	acc "deploy of $1: run $run succeeded"
}

acc_first_setup() {
	tui_log=$work/tui-screens.log
	: >"$tui_log"

	step 'sudo qdeploy opens the interface of the fresh service'
	tui_tmux kill-server >/dev/null 2>&1 || true
	tui_tmux new-session -d -s qdtui -x 120 -y 40 "env LANG=C.UTF-8 sh -c 'sudo qdeploy; echo qdeploy-exit=\$?; sleep 600'" ||
		fail 'cannot start tmux for the interface'
	tui_wait '1 Stacks' 30
	tui_wait 'No stacks' 30

	step 'Settings: the deploy key is generated and registered on the git server'
	tui_page 1
	tui_wait 'No deploy key yet' 10
	tui_key_generate
	tui_wait 'Add this public key' 10
	pub=$(tui_screen | grep -o 'ssh-ed25519 [A-Za-z0-9+/=]*' | head -n 1)
	[ -n "$pub" ] || fail "the public key is not on the screen: $(tui_screen)"
	printf '%s\n' "$pub" | docker exec -i "$prefix-git" sh -c 'cat >>/srv/git/.ssh/authorized_keys'
	acc 'deploy key generated through the interface and registered on the git server'

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

	step 'Settings: a webhook channel is added and its test message is sent'
	tui_page 4
	tui_wait 'Notifications' 10
	tui_send a
	tui_wait 'Add a notification channel' 10
	tui_type acceptance-hook
	tui_send F2
	tui_wait 'Settings of the channel' 10
	tui_type "https://hooks.$domain$acc_hook_path"
	tui_send Tab
	tui_type "$acc_hook_signing"
	tui_send F2
	tui_wait 'Add the channel acceptance-hook' 10
	tui_send y
	tui_wait 'The channel acceptance-hook is added' 20
	tui_send Enter
	tui_wait 'acceptance-hook' 10
	tui_send s
	tui_wait 'Send a test message through acceptance-hook?' 10
	tui_send y
	tui_wait 'was delivered' 60
	tui_send Enter
	hook_seen "$acc_hook_path" test || fail 'the webhook receiver did not record the test message of the interface'
	acc 'webhook channel added through the interface; the test message reached the receiver'

	step 'Settings: the repository is bound after the host key is shown and confirmed'
	tui_page 0
	tui_wait 'No stacks repository is bound' 10
	tui_send b
	tui_wait 'Bind the stacks repository' 10
	tui_type "git.$domain:stacks.git"
	tui_send Tab
	tui_type "$acc_stacks"
	tui_send F2
	tui_wait "Create the stack folder $acc_stacks?" 10
	if docker exec "$host" test -e "$acc_stacks"; then fail 'the stack folder exists before the question was answered'; fi
	tui_send y
	expected=$(git_ed25519_fingerprint)
	tui_wait "$(printf '%s' "$expected" | cut -c 1-30)" 60
	tui_wait 'ssh-ed25519' 10
	tui_send y
	tui_wait 'The stacks repository is bound' 120
	tui_send Enter
	docker exec "$host" test -f "$acc_stacks/n8n/compose.yaml" || fail "the clone is missing in $acc_stacks"
	[ "$(docker exec "$host" stat -c %U:%G:%a "$acc_stacks")" = qdeploy:qdeploy:755 ] ||
		fail "$acc_stacks is $(docker exec "$host" stat -c %U:%G:%a "$acc_stacks"), want qdeploy:qdeploy:755"
	docker exec "$host" systemctl show -p ReadWritePaths qdeploy | grep -qF "$acc_stacks" || fail "the service has no write access to $acc_stacks"
	acc "repository bound through the interface, host key $expected confirmed"

	step 'the Stacks area lists n8n and bookstack; their first runs end without secrets'
	tui_send 1
	tui_wait 'n8n' 60
	tui_wait 'bookstack' 60
	wait_for 'the first run of n8n' 180 stack_idle n8n
	wait_for 'the first run of bookstack' 180 stack_idle bookstack

	# n8n runs as the user node (UID 1000) and Docker would create a missing bind mount source as root, so its first
	# run waits until the directory exists with that owner; BookStack and MariaDB run as root.
	step 'the first run of n8n waits for its data directory'
	i=0
	until stack_field n8n findings | grep -q data-dir-missing; do
		i=$((i + 1))
		[ "$i" -lt 90 ] || fail "n8n has no data-dir-missing finding: $(qd --output json status) $(qd stack runs n8n) $(docker exec "$host" docker ps -a)"
		sleep 1
	done
	finding=$(stack_field n8n findings)
	case $finding in
	data-dir-missing:*' data (owner 1000:1000)'*) ;;
	*) fail "n8n has no finding for the directory data with the owner 1000:1000: $(qd --output json status)" ;;
	esac
	log "status of n8n: \"stack.n8n.findings\": \"$finding\""
	docker exec "$host" test ! -e "$acc_stacks/n8n/data" || fail 'the data directory of n8n exists before it was created'
	[ -z "$(docker exec "$host" docker ps -aq --filter label=com.docker.compose.project=n8n)" ] ||
		fail 'containers of n8n exist although its data directory is missing'
	acc "n8n waits with: $finding"

	step 'Stack detail: the secrets of secrets.example.env are set through the interface'
	for stack in n8n bookstack; do
		values=$(as_admin "cat ~/stacks/$stack/secrets.example.env")
		tui_send 1
		tui_wait 'CURRENT' 10
		acc_cursor_to "$stack"
		tui_send Enter
		tui_wait 'Secrets' 15
		while IFS= read -r line; do
			case $line in '' | '#'*) continue ;; esac
			acc_tui_secret "$stack" "${line%%=*}" "${line#*=}"
		done <<VALUES_END
$values
VALUES_END
		tui_send Escape
		stack_secrets=$(printf '%s\n' "$values" | grep -v '^#' | grep -v '^$' | sed "s/^\\([^=]*\\)=\\(.*\\)\$/\\1='\\2'/")
		[ "$(docker exec "$host" cat "$acc_stacks/$stack/secrets.env")" = "$stack_secrets" ] ||
			fail "secrets.env of $stack does not hold the example secrets"
		[ "$(docker exec "$host" stat -c %U:%a "$acc_stacks/$stack/secrets.env")" = qdeploy:600 ] ||
			fail "secrets.env of $stack has the wrong owner or mode"
	done
	acc 'secrets of n8n and bookstack set through the interface (mode 600, owner qdeploy)'

	step 'Stacks: bookstack is deployed without asking'
	tui_send 1
	acc_deploy_via_tui bookstack
	[ -z "$(stack_field bookstack findings)" ] || fail "bookstack has findings: $(stack_field bookstack findings)"
	acc 'bookstack deployed without a finding'

	step 'Stack detail: the data directory of n8n is created through the interface with the proposed owner'
	tui_send 1
	tui_wait 'needs data dirs' 30
	acc_cursor_to n8n
	prev=$(newest_run n8n)
	tui_send Enter
	tui_wait 'Missing data directories' 15
	tui_wait 'owner 1000:1000' 10
	tui_send m
	tui_wait 'Create data directories' 10
	tui_wait '1000:1000' 10
	tui_wait '0750' 10
	tui_send F2
	tui_wait 'Create the missing data directories of the stack' 10
	tui_send y
	tui_wait 'Data directories created for the stack n8n' 60
	tui_wait 'started at commit' 10
	tui_send Enter
	tui_send Escape
	owner_mode=$(docker exec "$host" stat -c '%u:%g %a' "$acc_stacks/n8n/data") || fail 'the data directory of n8n does not exist'
	log "stat of $acc_stacks/n8n/data: $owner_mode"
	[ "$owner_mode" = '1000:1000 750' ] || fail "the data directory of n8n has owner and mode '$owner_mode'"
	run=$(next_run n8n "$prev") || fail 'no run of n8n started after the creation'
	wait_for "the run $run" 600 run_finished n8n "$run"
	qd stack log "$run" | grep -q ' result succeeded' || fail "the run of n8n did not succeed: $(qd stack log "$run")"
	[ -n "$(docker exec "$host" docker ps -q --filter label=com.docker.compose.project=n8n --filter status=running)" ] ||
		fail 'n8n does not run after the creation'
	# The owner setup of the later steps is silently ignored by n8n while it still starts up.
	wait_for 'n8n to answer' 180 acc_healthy n8n
	wait_for 'n8n to be ready' 120 sh -c "[ \"\$(docker exec $host curl -s -o /dev/null -w %{http_code} http://127.0.0.1:5678/healthz/readiness)\" = 200 ]"
	acc "data directory of n8n created through the interface ($owner_mode); run $run succeeded and n8n runs"
	tui_send q
	tui_wait 'qdeploy-exit=0' 10
	tui_tmux kill-server >/dev/null 2>&1 || true
}

# acc_update_window moves the window to two minutes from now and waits until the service has updated the stacks.
acc_update_window() {
	step 'the update check shows the changelog of both stacks'
	out=$(qd --output json stack updates --refresh) || fail "updates --refresh failed: $out"
	for pair in "n8n 2.47.0 fixes an invented webhook timeout" "bookstack 26.9.1 an invented search ranking" "bookstack 11.8.9 an invented replication stall"; do
		set -- $pair
		stack=$1 version=$2
		shift 2
		out=$(qd --output json stack changelog "$stack") || fail "changelog of $stack failed: $out"
		printf '%s' "$out" | grep -qF "$*" || fail "the changelog of $stack does not hold the notes for $version: $out"
		acc "changelog of $stack shows the notes for $version: '$*'"
	done

	step 'the window opens and updates n8n, bookstack and its database by itself'
	head_before=$(as_admin 'cd ~/stacks && git rev-parse HEAD')
	started=$(status_field started_at)
	start=$(date -u -d '+2 minutes' +%H:%M)
	out=$(qd --output json window set --days daily --zone UTC --start "$start" --duration 25m) || fail "window set failed: $out"
	window_last_before=$(window_field last_run)
	wait_for 'the window to open' 180 sh -c "docker exec -i $host qdeploy --output json window show | grep -q '\"open\": *true'"
	wait_for 'the window work to finish' 1500 window_done
	[ "$(status_field started_at)" = "$started" ] || fail 'the service restarted'
	stack_images n8n | grep -qx "$N8N_IMAGE:2.47.0" || fail "n8n does not run 2.47.0: $(stack_images n8n)"
	stack_images bookstack | grep -qx "$BOOKSTACK_IMAGE:26.09.1" || fail "bookstack does not run 26.09.1: $(stack_images bookstack)"
	stack_images bookstack | grep -qx "$MARIADB_IMAGE:11.8.9" || fail "the database of bookstack does not run 11.8.9: $(stack_images bookstack)"
	wait_for 'n8n to answer' 120 acc_healthy n8n
	wait_for 'bookstack to answer' 120 acc_healthy bookstack
	acc_env_has n8n N8N_VERSION=2.47.0
	acc_env_has bookstack BOOKSTACK_VERSION=26.09.1
	acc_env_has bookstack MARIADB_VERSION=11.8.9
	for stack in n8n bookstack; do
		subjects=$(as_admin "cd ~/stacks && git log --format=%s $head_before..HEAD -- $stack")
		[ -n "$subjects" ] || fail "the window made no version commit for $stack"
		acc "version commits of $stack: $(printf '%s' "$subjects" | tr '\n' ';')"
	done
	acc_expect_items n8n "$acc_alpha"
	acc_expect_items bookstack "$acc_alpha"
	wait_for 'the webhook for the window update of n8n' 60 hook_seen "$acc_hook_path" succeeded n8n
	wait_for 'the webhook for the window update of bookstack' 60 hook_seen "$acc_hook_path" succeeded bookstack
	window_far_away >/dev/null || fail 'cannot move the maintenance window away'
	acc 'window update: n8n 2.47.0 and the database of bookstack 11.8.9, bookstack 26.09.1, healthy, test data kept, webhook heard of both'
}

# acc_failed_update updates the stack $1 into the test tag with the failing healthcheck, writes a marker file into
# its data once the new version has started, and requires the rollback to restore the data and revert the commit.
acc_failed_update() {
	stack=$1 var=$2 good=$3 bad=$4 image=$5 how=$6
	# The marker goes where the backup section of the stack settings saves data: all of data/ for n8n, data/config for bookstack.
	marker=$acc_stacks/$stack/data/late-marker
	[ "$stack" != bookstack ] || marker=$acc_stacks/$stack/data/config/late-marker
	prev=$(newest_run "$stack")
	rm -f "$work/update-$stack.rc"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	if [ "$how" = update ]; then
		( rc=0; qd stack update "$stack" >"$work/update-$stack.out" || rc=$?; echo $rc >"$work/update-$stack.rc" ) &
	else
		admin_commit "$stack/.env" "s/^$var=.*/$var=$bad/" "Update $stack $var from $good to $bad"
		restart_service
	fi
	run=$(next_run "$stack" "$prev") || fail "no run of $stack started"
	i=0
	until qd stack log "$run" | grep -q ' step up$'; do
		i=$((i + 1))
		[ "$i" -lt 600 ] || fail "the run $run never started the new version"
		sleep 0.5
	done
	docker exec "$host" sh -ec "printf late >$marker && chown 1000:1000 $marker" ||
		fail 'cannot write the marker into the data'
	if [ "$how" = update ]; then
		wait_for "the update of $stack to end" 900 test -s "$work/update-$stack.rc"
		[ "$(cat "$work/update-$stack.rc")" = 1 ] || fail "the update of $stack into $bad exited with $(cat "$work/update-$stack.rc")"
	fi
	wait_for "the run $run" 300 run_finished "$stack" "$run"
	run_log=$(qd stack log "$run")
	printf '%s' "$run_log" | grep -q ' result rolled-back' || fail "the run of $stack did not end rolled back: $run_log"
	steps=$(log_steps "$run_log")
	case $steps in *rollback,stop,restore,revert,push*) ;; *) fail "the rollback of $stack has the steps $steps" ;; esac
	acc "failed update of $stack to $bad: steps $steps"
	docker exec "$host" test ! -e "$marker" || fail "the restore left the marker in the data of $stack"
	as_admin 'cd ~/stacks && git pull -q --ff-only'
	acc_env_has "$stack" "$var=$good"
	[ "$(as_admin "git ls-remote $repo refs/heads/main" | cut -f 1)" = "$(as_admin 'cd ~/stacks && git rev-parse HEAD')" ] ||
		fail 'the remote branch differs from the clone'
	subject=$(as_admin "cd ~/stacks && git log -1 --format=%s -- $stack")
	case $subject in Revert*) ;; *) fail "the newest commit of $stack is not a revert: $subject" ;; esac
	acc "revert commit of $stack on the remote: $subject"
	stack_images "$stack" | grep -qx "$image:$good" || fail "$stack does not run $good again: $(stack_images "$stack")"
	wait_for "$stack to answer" 120 acc_healthy "$stack"
	acc_expect_items "$stack" "$acc_alpha"
	wait_for "the webhook for the rollback of $stack" 60 hook_seen "$acc_hook_path" rolled-back "$stack"
	acc "$stack runs $good again with the test data and the webhook heard of the rollback"
}

# acc_restore_older creates two backups of the stack $1 with a change between them, restores the older one and
# requires its data back and the owners and modes of the data tree as before.
acc_restore_older() {
	stack=$1
	out=$(qd --output json stack backup create "$stack") || fail "backup create failed: $out"
	older=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	wait_for "$stack to answer" 120 acc_healthy "$stack"
	acc_tree "$stack" >"$work/tree-$stack-before"
	acc_add "$stack" "$acc_beta"
	acc_expect_items "$stack" "$acc_alpha,$acc_beta"
	# Backup ids start with the second of their creation; two in one second would not be ordered by age.
	sleep 2
	out=$(qd --output json stack backup create "$stack") || fail "backup create failed: $out"
	newest=$(printf '%s\n' "$out" | sed -n 's/.*"backup": *"\([^"]*\)".*/\1/p')
	[ -n "$older" ] && [ "$older" != "$newest" ] || fail "no two backups: $older, $newest"
	wait_for "$stack to answer" 120 acc_healthy "$stack"
	out=$(qd stack backup restore "$stack" "$older" --yes) || fail "restore failed: $out"
	wait_for "$stack to answer after the restore" 180 acc_healthy "$stack"
	acc_expect_items "$stack" "$acc_alpha"
	acc_tree "$stack" >"$work/tree-$stack-after"
	[ "$(wc -l <"$work/tree-$stack-before")" -gt 3 ] || fail "the data tree of $stack is too small to compare"
	awk -F '\t' 'NR == FNR { a[$1] = $2; next } ($1 in a) && a[$1] != $2 { print $1, a[$1], "->", $2; bad = 1 } END { exit bad }' \
		"$work/tree-$stack-before" "$work/tree-$stack-after" || fail "owners or modes of the data of $stack changed"
	acc "restore of the older backup $older of $stack: '$acc_beta' gone, '$acc_alpha' there; owner:group:mode of $(wc -l <"$work/tree-$stack-before") paths in data/ unchanged"
}

# acc_slow_git makes the git server slow to pack objects when $1 is on and restores it when $1 is off. The
# restore leaves the log of the slow packings in $slow.
acc_slow_git() {
	if [ "$1" = on ]; then
		docker exec "$prefix-git" sh -ec '
			printf "#!/bin/sh\necho slow >>/tmp/qdeploye2e-slow-pack.log\nsleep 15\nexec \"\$@\"\n" >/usr/local/bin/qdeploye2e-slow-pack
			chmod 755 /usr/local/bin/qdeploye2e-slow-pack
			rm -f /tmp/qdeploye2e-slow-pack.log
			git config --system uploadpack.packObjectsHook /usr/local/bin/qdeploye2e-slow-pack'
		return
	fi
	docker exec "$prefix-git" sh -ec 'git config --system --unset uploadpack.packObjectsHook; rm -f /usr/local/bin/qdeploye2e-slow-pack'
	slow=$(docker exec "$prefix-git" cat /tmp/qdeploye2e-slow-pack.log 2>/dev/null || true)
	docker exec "$prefix-git" rm -f /tmp/qdeploye2e-slow-pack.log
}

# acc_push_note pushes a commit with a new note, so that the next fetch has objects to pack.
acc_push_note() {
	as_admin "cd ~/stacks && git pull -q --ff-only && printf '$1\n' >note.txt && git add note.txt &&
		git -c user.name='E2E Admin' -c user.email=admin.qdeploye2e.test commit -q -m 'Add a note' && git push -q origin main"
}

# acc_list_wait times sudo qdeploy stack list. It sets $waited and $list_rc and leaves the verdict to
# acc_list_verdict, so the git server can be restored first.
acc_list_wait() {
	list_start=$(date +%s%N)
	list_rc=0
	as_admin 'sudo qdeploy stack list' >/dev/null || list_rc=$?
	list_end=$(date +%s%N)
	waited=$(LC_ALL=C awk -v a="$list_start" -v b="$list_end" 'BEGIN { printf "%.1f", (b - a) / 1e9 }')
}

# acc_list_verdict fails unless the stack list during $1 succeeded within 5 seconds.
acc_list_verdict() {
	[ "$list_rc" = 0 ] || fail "stack list failed during $1"
	LC_ALL=C awk -v w="$waited" 'BEGIN { exit !(w <= 5) }' || fail "stack list waited ${waited}s during $1; the limit is 5s"
}

# acc_lock_wait makes the git server slow to pack objects and measures how long a stack list waits during a
# fetch and during a poll of the engine; each wait must stay within 5 seconds.
acc_lock_wait() {
	step 'the git server is made slow, a fetch starts, and sudo qdeploy stack list is timed'
	acc_slow_git on
	acc_push_note 'invented note'
	rm -f "$work/fetch.rc"
	fetch_start=$(date +%s%N)
	( rc=0; qd repo fetch >"$work/fetch.out" || rc=$?; echo $rc >"$work/fetch.rc" ) &
	sleep 2
	acc_list_wait
	wait_for 'the fetch to end' 120 test -s "$work/fetch.rc"
	fetch_end=$(date +%s%N)
	acc_slow_git off
	[ -n "$slow" ] || fail 'the fetch did not go through the slow packing of the git server'
	[ "$(cat "$work/fetch.rc")" = 0 ] || fail "the fetch failed: $(cat "$work/fetch.out")"
	acc_list_verdict 'a fetch'
	fetched=$(LC_ALL=C awk -v a="$fetch_start" -v b="$fetch_end" 'BEGIN { printf "%.1f", (b - a) / 1e9 }')
	acc "stack list waited ${waited}s during a fetch (the fetch took ${fetched}s)"

	step 'the git server is made slow, the engine polls after a restart, and sudo qdeploy stack list is timed'
	acc_slow_git on
	acc_push_note 'another invented note'
	restart_service
	wait_for 'the poll to reach the slow packing' 20 docker exec "$prefix-git" test -s /tmp/qdeploye2e-slow-pack.log
	polling=
	poll_done || polling=yes
	acc_list_wait
	wait_for 'the poll to end' 120 poll_done
	acc_slow_git off
	[ -n "$polling" ] || fail 'the poll was over before the stack list started'
	acc_list_verdict 'a poll'
	acc "stack list waited ${waited}s during a poll of the engine"
}

run_acceptance() {
	repo=git.$domain:stacks.git
	N8N_IMAGE=registry.$domain/n8nio/n8n
	BOOKSTACK_IMAGE=registry.$domain/linuxserver/bookstack
	MARIADB_IMAGE=registry.$domain/library/mariadb

	step 'the stacks repository holds only n8n and bookstack, both in automatic mode'
	as_admin "rm -rf ~/stacks && git clone -q $repo ~/stacks &&
		cd ~/stacks && git rm -rq demo-stack demo-cache demo-badpath demo-plain .qatlas/qdeploy/demo-stack.yaml .qatlas/qdeploy/demo-badpath.yaml &&
		sed -i 's/^update:\$/update:\n  mode: automatic/' .qatlas/qdeploy/n8n.yaml .qatlas/qdeploy/bookstack.yaml &&
		git -c user.name='E2E Admin' -c user.email=admin.qdeploye2e.test commit -q -am 'Keep n8n and bookstack in automatic mode' &&
		git push -q origin main" || fail 'cannot prepare the stacks repository'
	[ "$(as_admin "ls ~/stacks | tr '\n' ' '")" = 'bookstack n8n ' ] || fail 'the stacks repository does not hold only n8n and bookstack'
	[ "$(as_admin "ls ~/stacks/.qatlas/qdeploy | tr '\n' ' '")" = 'bookstack.yaml n8n.yaml ' ] || fail 'the stacks repository does not hold only the settings of n8n and bookstack'

	setup_service default-backup-image
	if docker exec "$host" test -e /etc/systemd/system/qdeploy.service.d; then fail 'the unit has a drop-in'; fi
	exec_start=$(docker exec "$host" systemctl show -p ExecStart --value qdeploy)
	case $exec_start in *backup-image*) fail "the unit passes a backup image: $exec_start" ;; esac
	if docker exec "$host" test -e "$acc_stacks"; then fail "the installation created the stack folder $acc_stacks"; fi
	[ "$(docker exec "$host" systemctl is-active qdeploy)" = active ] || fail 'the service does not run after the installation'
	acc 'installed from the test release with the installer; the service runs without --backup-image and without a stack folder'

	acc_first_setup

	step 'the test data is created in both stacks'
	acc_n8n POST /rest/owner/setup \
		"{\"email\":\"$acc_owner_email\",\"firstName\":\"Invented\",\"lastName\":\"Owner\",\"password\":\"$acc_owner_password\"}" >/dev/null ||
		fail 'n8n owner setup failed'
	acc_add n8n "$acc_alpha"
	acc_add bookstack "$acc_alpha"
	acc_expect_items n8n "$acc_alpha"
	acc_expect_items bookstack "$acc_alpha"
	for stack in n8n bookstack; do
		wait_for "$stack to answer" 120 acc_healthy "$stack"
	done
	acc "healthy; test data: n8n workflow '$acc_alpha', bookstack book '$acc_alpha'"

	acc_update_window

	step 'the test images with a failing healthcheck reach the registry'
	push_broken_images
	out=$(qd --output json stack updates --refresh) || fail "updates --refresh failed: $out"
	printf '%s' "$out" | grep -qF 2.48.0 || fail "the update check does not offer n8n 2.48.0: $out"

	step 'an update into a failing version restores the data, reverts the commit and runs the previous version'
	acc_failed_update n8n N8N_VERSION 2.47.0 2.48.0 "$N8N_IMAGE" update
	acc_failed_update bookstack BOOKSTACK_VERSION 26.09.1 26.10.0 "$BOOKSTACK_IMAGE" update

	step 'an older backup is restored and the owners and modes of the data stay'
	acc_restore_older n8n
	acc_restore_older bookstack

	step 'backups ran with the default helper image, and the database dump of bookstack made by the service user holds the data'
	docker exec "$host" docker image ls --digests --format '{{.Repository}}@{{.Digest}}' | grep -qx "$(backup_helper_ref)" ||
		fail "the host does not hold the default helper image $(backup_helper_ref)"
	dump=$(docker exec "$host" sh -c 'ls -d /var/lib/qdeploy/backups/bookstack/*/command.out | tail -n 1')
	[ "$(docker exec "$host" stat -c %U "$dump")" = qdeploy ] || fail "the dump $dump does not belong to the service user"
	docker exec "$host" grep -q "$acc_alpha" "$dump" && docker exec "$host" grep -q 'CREATE TABLE' "$dump" ||
		fail "the database dump $dump does not hold the test data"
	acc "default helper image $(backup_helper_ref) in use; database dump of bookstack made by qdeploy: $(docker exec "$host" stat -c %s "$dump") bytes with the test data"

	acc_lock_wait

	step 'the service is stopped'
	docker exec "$host" systemctl stop qdeploy || fail 'cannot stop the qdeploy service'
	[ "$(docker exec "$host" systemctl is-active qdeploy || true)" = inactive ] || fail 'the qdeploy service is still active'
	docker exec "$host" rm -f "$acc_jar"
	acc 'service stopped'
	log 'acceptance: passed'
}
