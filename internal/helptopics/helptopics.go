// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qdeploy", Short: "What qdeploy is and what it is for", Text: overview},
		{Name: "credentials", Short: "The deploy key and stored credentials", Text: credentialsTopic},
		{Name: "repository", Short: "Binding the stacks repository and trusting its host key", Text: repositoryTopic},
		{Name: "stack-standard", Short: "The stacks-repo standard that qdeploy stack check enforces", Text: stackStandardTopic},
		{Name: "stack-rules", Short: "The qdeploy.yaml rule file of a stack", Text: stackRulesTopic},
		{Name: "backups", Short: "Backing up and restoring the data of a stack", Text: backupsTopic},
		{Name: "notifications", Short: "Notification channels: events, types and secrets", Text: notificationsTopic},
		{Name: "containers", Short: "The containers of the host: kinds, states, metrics, start, stop, restart", Text: containersTopic},
	}
}

const overview = `qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and up to date from its stacks repository.

Commands:
- qdeploy run: run the service in the foreground until it receives SIGTERM or SIGINT
- qdeploy tui, or qdeploy without a command at a terminal: open the terminal interface with the areas Stacks, Stack detail, Runs and Settings; it shows each stack's versions, state, last run and findings, follows the log of a running run, and offers update, deploy, pause, pin, resume and the registry query on the cursor stack or on stacks ticked with space; the stack detail sets and deletes the stack's secrets (value hidden) and restores an older backup; Settings binds the repository, generates and shows the deploy key, sets credentials, changes the maintenance window and adds, tests and removes notification channels; every change comes after a confirmation (run with sudo)
- qdeploy status: show whether the service is running, its version and start time, the last poll and the state of each stack (run with sudo)
- qdeploy key generate|show: create the SSH deploy key and show its public part (run with sudo)
- qdeploy repo set <ssh-url> [--dir <path>]: bind the stacks repository, confirm its host key and clone it (run with sudo)
- qdeploy repo fetch: fetch new commits without touching the working copy (run with sudo)
- qdeploy stack list: list the stacks of the working copy with image versions, update mode and whether the stack is stopped (run with sudo)
- qdeploy stack check [stack]: check the stacks of the working copy against the stacks-repo standard; exit code 1 on errors (run with sudo)
- qdeploy stack deploy <stack>: start a manual run of a stack at the commit the working copy has (run with sudo)
- qdeploy stack runs [stack]: list the recorded runs, newest first (run with sudo)
- qdeploy stack log <run>: print the log of a run (run with sudo)
- qdeploy stack resume <stack>: clear the pause a failed rollback left on a stack; it starts no run (run with sudo)
- qdeploy stack pause|unpause <stack>: keep the maintenance window from running a stack, or lift that; unrelated to the pause of a failed rollback (run with sudo)
- qdeploy stack pin|unpin <stack>: hold the image versions of a stack, so the maintenance window and qdeploy stack update leave them alone, or release them (run with sudo)
- qdeploy stack start|stop|restart <stack> [--yes]: start, stop or restart the containers of a stack after a question; a stop keeps the stack stopped against new commits and the maintenance window until a start, and qdeploy status and stack list show it (run with sudo; see qdeploy help containers)
- qdeploy window show|set: show the maintenance window, in which the service checks the registries and updates the stacks in automatic mode, or change its weekdays, start, duration and time zone; the running service follows at once (run with sudo)
- qdeploy stack update <stack>... [--version <v>]: update the image versions of stacks, each version step a commit of the stack's .env pushed to the branch and a run with backup, check and rollback (run with sudo)
- qdeploy stack updates [stack] [--refresh]: show the current and the available image versions per stack and variable, with newer major versions flagged as not automatic; --refresh asks the registries now (run with sudo)
- qdeploy stack changelog <stack>: show the release notes between the current and the available version of each variable with an update; qdeploy stack updates --changelog shows them for all stacks (run with sudo)
- qdeploy stack backup create|list <stack>, qdeploy stack backup restore <stack> <backup> [--yes]: back up, list and restore the data of a stack (run with sudo; see qdeploy help backups)
- qdeploy stack secret set <stack> <KEY>, qdeploy stack secret list <stack>, qdeploy stack secret delete <stack> <KEY>: write, list by key and remove the secrets in a stack's secrets.env; the value is read without echo at a terminal or from standard input, never from an argument, and nothing is redeployed (run with sudo)
- qdeploy credential set|list|delete: manage registry tokens and notification credentials (run with sudo)
- qdeploy notify list|add|remove|test: manage the notification channels (webhook, Slack, Telegram, e-mail) and send a test message through one (run with sudo; see qdeploy help notifications)
- qdeploy update [version], qdeploy update status: update qdeploy itself to the latest stable release or to a given newer one from its signed GitHub releases and restart the service; status shows the installed version and the latest release and changes nothing (update runs with sudo and needs --confirm; stack updates are qdeploy stack update)
- qdeploy container list [--metrics], qdeploy container show <name>: show every container of the host, own stacks, foreign Compose projects and single containers, with state; changes nothing (run with sudo; see qdeploy help containers)
- qdeploy container start|stop|restart <name> [--yes]: start, stop or restart one container of the host after a question (run with sudo; see qdeploy help containers)
- qdeploy version: print the version
- qdeploy help <topic>: show a help topic`

const credentialsTopic = `The service keeps its own secrets in a private store that only the service user can read. The commands need sudo because they talk to the service over its root-only channel.

Deploy key:
- qdeploy key generate: create an Ed25519 key in the service and print its public part and SHA256 fingerprint
- qdeploy key generate --replace: replace an existing key; without --replace an existing key is kept and the command refuses
- qdeploy key show: print the public part again
- Enter the public part as a deploy key in the git platform. The private key never leaves the service.

Credentials:
- qdeploy credential set <name>: store a registry token or notification credential; the value is read without echo at a terminal, or from standard input when piped, and never from an argument
- qdeploy credential list: show names and modification times, never values
- qdeploy credential delete <name>: remove a credential
- A name has 1 to 64 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter.
- Registry login for update checks: name the credential "registry." plus the registry host in lowercase with ':' written as '-' (Docker Hub: registry.docker.io), with the value <user>:<token>. Without such a credential the registry is asked anonymously.
- GitHub token for changelogs: name the credential "github.com", with the token alone as value. It is sent only to the GitHub API and raises the rate limit; without it GitHub is asked anonymously.
- A stored value is not shown again and is removed from the service's log output.`

const repositoryTopic = `The service clones the stacks repository of the host over SSH with its own deploy key and checks the server with its own known_hosts file; the system's SSH configuration and known hosts are not used. Host key checking is always strict.

Binding:
- qdeploy key generate, then enter the public key as a deploy key with write access in the git platform; qdeploy pushes its version and revert commits with it
- qdeploy repo set <ssh-url> --dir <path>: the URL is user@host:path or ssh://[user@]host[:port]/path; --dir names the stack folder, is required and has no default
- On first contact the host key and its SHA256 fingerprint are shown. At a terminal you confirm it by answering the question (default no). Without a terminal, pass the expected fingerprint: --fingerprint SHA256:...
- Compare the fingerprint with the one the git platform publishes before you confirm it.
- The stack folder is any absolute path, for example on another disk; each host chooses its own. It must be empty or already a clone of the same URL. A repository bound earlier is not replaced.
- A folder that does not exist is created only after you confirmed its exact path: answer the question at a terminal, or pass --create without one. It gets the owner qdeploy:qdeploy and mode 0755. Neither the installer, the service nor an update creates a stack folder.
- The service runs sandboxed and may write only where the systemd drop-in /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf allows it. repo set adds the folder there as ReadWritePaths=-<path> (entries are only ever added), reloads systemd and restarts the service once. A bound folder without that entry, as after an update from an older release, is added when you next run qdeploy as root.
- The same steps are offered by sudo qdeploy in the Settings area.

Afterwards:
- qdeploy repo fetch: fetch new commits into the remote-tracking branch and report whether the working copy is behind; the working copy itself is not changed
- qdeploy stack list: list each stack with its image version variables, their values and its update mode (none without a qdeploy.yaml)
- A host key that differs from the pinned one aborts every command. The pinned key is never replaced by a command; verify the change with the git platform's operator first.`

const stackRulesTopic = `Each stack folder of the stacks repository may hold a qdeploy.yaml with the rules for updating it. Without the file the stack is deployed but never updated. The file is read as data only: unknown fields, a missing version, or a path that is absolute or leaves the stack folder are rejected.

Fields (all optional except version):
- version: 1
- update.mode: manual (default) or automatic
- update.ceiling: patch, minor (default) or major; the largest version step taken
- update.major: stepwise (default, one major version at a time) or direct
- update.tags.<VARIABLE>: regular expression a candidate tag must match; VARIABLE is the image version variable in .env
- update.changelog.<VARIABLE>: GitHub repository (owner/name or a github.com address) whose releases are shown as changelog; it takes precedence over the image label org.opencontainers.image.source
- backup.paths: data directories below data/ in the stack folder, for example data/db; other paths are refused when a backup runs
- backup.volumes: volume keys of the Compose file (the top-level volumes: names), not Docker volume names
- backup.command: a command as a list of arguments, never a shell line (see qdeploy help backups)
- backup.retention.keep: number of newest backups to keep (default 7 when no retention is set)
- backup.retention.max_age_days: age limit in days; a backup is kept only while it satisfies every limit that is set
- check.containers: require all containers to be running (default true)
- check.http.url: http or https URL to probe
- check.http.status: expected status code (default 200)
- check.http.timeout: probe timeout from 1s to 10m (default 10s)
- backup needs at least one of paths, volumes, command

Example:
version: 1
update:
  mode: automatic
  ceiling: minor
  tags:
    DEMO_APP_VERSION: '^[0-9]+\.[0-9]+\.[0-9]+$'
backup:
  paths: [data/db]
  retention: {keep: 5}
check:
  http: {url: 'http://localhost:8080/health', status: 200, timeout: 5s}

Image versions stand as variables in the stack's .env, for example image: registry.example.test/demo/app:${DEMO_APP_VERSION}. qdeploy stack check reports fixed and floating tags such as latest (see qdeploy help stack-standard).`

const backupsTopic = `A backup saves the data of one stack while the stack is stopped, so the copy is consistent. What is saved is listed in the backup section of the stack's qdeploy.yaml (see qdeploy help stack-rules). Backups are kept in the service's state directory under backups/<stack>/<backup>, readable by the service user only.

Commands (run with sudo; the service must run):
- qdeploy stack backup create <stack>: stop the stack if it runs, save every listed data directory and volume, run the backup command, start the stack again, then remove backups beyond the retention limits
- qdeploy stack backup list <stack>: the backups of a stack, newest first, with time, deployed commit, size and number of parts
- qdeploy stack backup restore <stack> <backup> [--yes]: replace the data with the content of one backup; the stack is stopped and started again if it ran

How it works:
- A data directory or volume is packed into a tar archive with owners and permissions by a short-lived helper container without network. The helper image is set with qdeploy run --backup-image; it needs sh, find, rm and a tar that knows --numeric-owner.
- backup.command runs on the host, not in a container, as a program in the search path or an absolute path, in the stack folder and without a shell; its standard output is saved as command.out in the backup. A relative path such as ./dump.sh is refused.
- The backup records the stack, the time, the commit deployed at that time and the image versions.
- A failed backup is removed and a stack that ran is started again.
- A backup and a deployment of the same stack never run at the same time.
- Every deployment run of a stack with backup rules makes a backup after it stopped the stack; when the run is rolled back, that backup is restored. These backups count for retention like any other.

Retention:
- backup.retention.keep and backup.retention.max_age_days apply after each successful backup; a backup is kept only while it satisfies every limit that is set. The newest backup is never removed.

Restore:
- The backup id comes from qdeploy stack backup list; the newest is never chosen for you.
- At a terminal the command asks first (default no). Without a terminal, and with --agent, it refuses unless --yes is given.
- Every archive is read through before the first target is emptied. Each target is then emptied and unpacked inside the helper container, on the mounted path only.
- A backup that holds a data directory or volume the rules no longer list is refused as a whole.
- The command output is not restored; qdeploy.yaml has no restore command.
- A restore that fails after the first target was emptied leaves the stack stopped, because it would run on incomplete data.
- A volume must exist before it is backed up or restored; deploy the stack first.`

const stackStandardTopic = `qdeploy stack check compares the working copy of the stacks repository with this standard. It reports each deviation as a finding with stack, rule, severity, cause and fix. Errors break or endanger a deploy or leak secrets; warnings leave the stack working. The exit code is 1 when there is at least one error.

Layout of a stack folder:
- compose.yaml, optionally more compose files such as compose.prod.yaml
- .env: non-secret values such as image versions, domains, ports and COMPOSE_FILE
- secrets.env: the secrets; never versioned, listed in .gitignore, present on the host only
- data (volumes, databases, uploads) never lives in the repository

Rules (rule ID, severity):
- fixed-image-tag (error): an image version is written into a compose file; use a variable from .env
- image-version-default (warning): the version only comes from a default in the compose file; set it in .env
- floating-image-tag (error): a tag such as latest, no tag, or a tag without a SemVer core (x.y.z)
- unset-image-variable (error): the version variable has no value
- duplicate-key (error): a key stands in both .env and secrets.env; keep it in one file
- missing-secrets-env (error): secrets.env does not exist; Compose runs with --env-file .env --env-file secrets.env
- unreadable-secrets-env (error): secrets.env is a link or too large
- tracked-secrets-env (error): git tracks secrets.env
- secrets-env-not-ignored (warning): .gitignore does not cover secrets.env
- tracked-data (error): git tracks files below data/
- missing-compose-file (error): a file named in COMPOSE_FILE does not exist
- invalid-compose-file (error): a COMPOSE_FILE entry is unsafe or not a regular file
- invalid-rules-file (error): qdeploy.yaml is invalid
- unknown-rules-variable (error): qdeploy.yaml names an image variable the stack does not have
- unreadable-stack (error): another file of the stack cannot be read

Secret values are never shown or logged; the check compares only the key names of secrets.env with .env.`

const notificationsTopic = `A notification channel receives the events it selected. Channels belong to the host, not to a stack. Commands (run with sudo; the service must run):
- qdeploy notify list: the channels with type, events and settings, never their secrets
- qdeploy notify add <name> --type webhook|slack|telegram|smtp [--events <list>] [settings]: add a channel
- qdeploy notify remove <name>: remove a channel and its stored secrets
- qdeploy notify test <name>: send one test message once, without retries; a failure carries the error class of the delivery (auth, permission, not-found, rate-limited, unreachable, timeout, tls, config-invalid, invalid-request or runtime) in its message and exit code

Events (--events takes a comma list; without it a new channel receives all; a stored channel keeps the events it selected):
- succeeded: a run deployed a commit
- failed: a run failed and no rollback was made
- rolled-back: a run failed and the stack was rolled back to the previous state
- rollback-failed: a run failed and the rollback failed too; the stack is stopped and paused
- container-failed: the failure monitor found a container of an own stack failed (see qdeploy help containers); one message per failure with stack, kind, container and the time since which it failed
- recovered: that failure ended; names the same container and how long it lasted
- update-available: a newer image version was found for a stack in manual mode, once per version

Channel types and settings:
- webhook: posts JSON (event, stack, run, commit, error, items; for container-failed and recovered kind, container, since and, on recovered, at) to an https URL (plain http only to the local machine), signed with a key. The headers X-Qdeploy-Event, X-Qdeploy-Timestamp and X-Qdeploy-Signature carry the event, the Unix time and the signature over the timestamp and the body.
- slack: posts text to an incoming-webhook URL.
- telegram: sends text to a chat; needs --chat-id, and --base-url for a Bot API server of your own.
- smtp: sends an e-mail; needs --host, --from and --to (comma list or repeated); --port (default 587 with starttls, 465 with tls), --security starttls (default) or tls, and --user for a login. The connection is always encrypted.

Secrets: the webhook URL and its signing key, the Slack URL, the Telegram bot token and the SMTP password. They are never an argument or a flag value. At a terminal they are asked for without echo; piped on standard input they are lines in that order (webhook: URL, then key; smtp: the password only with --user). The service stores them as credentials, shows them nowhere and removes them from its log output.

Delivery runs in the background after the event; a failing channel is retried a few times and never delays or fails a run. A delivery that still fails is noted in the log of its run (qdeploy stack log <run>) and in the service log.`

const containersTopic = `qdeploy container list and qdeploy container show read the containers of the whole host from the container engine and change nothing. qdeploy container start, stop and restart act on one container. Run them with sudo; the service must run.

Commands:
- qdeploy container list [--metrics]: every container, grouped; add --output json for the service's result as it is
- qdeploy container show <name>: one container by name or ID, with its group, state, ports, mounts, networks, labels and the names of its environment variables
- qdeploy container start|stop|restart <name> [--yes]: act on one container by name or ID, also a foreign or single one

Kinds of a group:
- own: a Compose project that is a stack of the bound stacks repository and was started from that stack's folder
- foreign: any other Compose project; it shows its working directory and compose files
- single: a container that belongs to no Compose project
- conflict: a foreign project that has the name of a stack of this host but was started from another folder; it is not the stack and is marked in the list

State of a group is the worst state of its containers, in this order: unhealthy, restarting, stopped, starting, ok. The uptime of a group is that of its youngest running container; restarts are summed.

Actions: the command names the container and its kind and asks first (default no); --yes answers the question, and without a terminal --yes is required. A stop signals the container and kills it after ten seconds; stopping a stopped or starting a running container changes nothing. After a start or restart the service waits up to a minute until the container runs and, if it has a health check, is healthy; if it is not ready by then the command shows the state and fails. A container of a stack of this host is refused while a run of that stack is active.

Stacks: qdeploy stack start|stop|restart <stack> [--yes] act on all containers of an own stack of the working copy, ask first like the container actions and are refused while a run of the stack is active. A stop stops the containers without removing them and sets the mark "stopped": new commits of the stacks repository start no run (the stack shows the finding stopped and its pending commit), the maintenance window skips it, and qdeploy status and qdeploy stack list show "stopped since <time>". Only qdeploy stack start clears the mark; it starts the existing containers, never creates any (a stack without containers fails; deploy it with qdeploy stack deploy), clears the mark once Compose has started them and then runs the stack's check. qdeploy stack restart restarts the containers and runs the check; on a stack stopped through qdeploy it is refused with the hint to use start. Stopping a single container with qdeploy container stop sets no mark. A stopped stack is refused by every command that would run it (qdeploy stack deploy, qdeploy stack update, and likewise the maintenance window, which skips it); start it first.

Metrics: --metrics adds CPU (100 per fully used CPU) and memory of the running containers. Reading them takes about two seconds per running container, in parallel and within a time limit, so the command may wait up to two minutes. If a reading fails or the time runs out, the list says that the metrics are incomplete and the container shows none.

Container engine: the service reads the engine's socket, by default /var/run/docker.sock; qdeploy run --docker-socket <path> names another one. If the engine cannot be reached, the service user may not use it, or it does not answer in time, the command says so and shows nothing.

Failure monitor: every 30 seconds the service checks the containers of own stacks and reports a failure once, as the finding container-failed in qdeploy status and as the notification event container-failed; when the failure ends it sends recovered. The kinds:
- exited: a container exited with a non-zero code or is dead (an exit with code 0, such as an init job, is fine)
- unhealthy: its health check has been failing for more than 60 seconds
- restart-loop: its restart count rose by 3 or more within 5 minutes
- missing: a service that was running at the last good reading is gone
Nothing is checked while a run or an action of the stack is active, nor for stopped (qdeploy stack stop), paused (either pause) and foreign stacks. A container stopped with qdeploy container stop is no failure. The monitor only reports; it never acts on a container.

Environment: only the names of the variables are shown, never their values.`
