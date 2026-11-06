// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qdeploy", Short: "What qdeploy is and what it is for", Text: overview},
		{Name: "credentials", Short: "The deploy keys and stored credentials", Text: credentialsTopic},
		{Name: "repository", Short: "Binding the stacks repository and trusting its host key", Text: repositoryTopic},
		{Name: "sources", Short: "Several stack folders on one host: ids, deploy keys and unique stack names", Text: sourcesTopic},
		{Name: "stack-capabilities", Short: "What qdeploy can do with a stack's files, and what qdeploy stack check reports", Text: stackCapabilitiesTopic},
		{Name: "stack-settings", Short: "Stack settings", Text: stackSettingsTopic},
		{Name: "backups", Short: "Backing up and restoring the data of a stack", Text: backupsTopic},
		{Name: "data-directories", Short: "Missing data directories of a stack: the data-dir-missing finding and how to resolve it", Text: dataDirectoriesTopic},
		{Name: "notifications", Short: "Notification channels: events, types and secrets", Text: notificationsTopic},
		{Name: "containers", Short: "The containers of the host: kinds, states, metrics, start, stop, restart", Text: containersTopic},
	}
}

const overview = `qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and up to date from its stacks repository.

Commands:
- qdeploy run: run the service in the foreground until it receives SIGTERM or SIGINT
- qdeploy tui, or qdeploy without a command at a terminal: open the terminal interface with the areas Stacks, Stack detail, Runs, Settings and Containers; it shows each stack's versions, state, last run and findings, follows the log of a running run, and offers update, deploy, pause, pin, resume, start, stop and restart (a stopped stack shows as stopped) and the registry query on the cursor stack or on stacks ticked with space; the stack detail sets and deletes the stack's secrets (value hidden) and restores an older backup; Settings binds the repository, generates and shows the deploy key, sets credentials, changes the maintenance window and adds, tests and removes notification channels; Containers lists every container of the host by own stack, foreign project and single container with state and a detail, and reads again every 5 seconds while it is shown, and S, X and Z there start, stop and restart the container on the cursor or in the detail after a question, also a foreign or single one; the stack detail lists the containers of its stack; o opens the output of the container on the cursor, or in the stack detail of all containers of the stack, which follows once a second and takes a period with T and a search with /; every change comes after a confirmation (run with sudo)
- qdeploy status: show whether the service is running, its version and start time, the last poll and the state of each stack (run with sudo)
- qdeploy key generate|show [--dir <path>]: create the SSH deploy key of a stack folder and show its public part (run with sudo)
- qdeploy repo set <ssh-url> [--dir <path>]: bind a stacks repository to a stack folder, confirm its host key and clone it; a further folder becomes a further source (run with sudo; see qdeploy help sources)
- qdeploy repo fetch: fetch new commits of every stack folder without touching the working copies (run with sudo)
- qdeploy stack list: list the stacks of the working copy with image versions, update mode and whether the stack is stopped (run with sudo)
- qdeploy stack check [stack]: show per stack of the working copy whether deploy, update, rollback with data, backup and the redaction of secrets work with its files, why not and what to change, and the findings; exit code 1 on errors (run with sudo; see qdeploy help stack-capabilities)
- qdeploy stack deploy <stack>: start a manual run of a stack at the commit the working copy has; the stack is named by its Compose project (see qdeploy help stack-capabilities), and for a stack with the finding project-renamed this decides to start the new project, next to the old one, which keeps running (run with sudo)
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
- qdeploy stack datadir <stack> [--owner UID:GID] [--mode 0750] --yes: create the missing data directories of a stack and deploy it (run with sudo; see qdeploy help data-directories)
- qdeploy stack secret set <stack> <KEY>, qdeploy stack secret list <stack>, qdeploy stack secret delete <stack> <KEY>: write, list by key and remove the secrets in a stack's secrets.env; the value is read without echo at a terminal or from standard input, never from an argument, and nothing is redeployed (run with sudo)
- qdeploy credential set|list|delete: manage registry tokens and notification credentials (run with sudo)
- qdeploy notify list|add|remove|test: manage the notification channels (webhook, Slack, Telegram, e-mail) and send a test message through one (run with sudo; see qdeploy help notifications)
- qdeploy update [version], qdeploy update status: update qdeploy itself to the latest stable release or to a given newer one from its signed GitHub releases and restart the service; status shows the installed version and the latest release and changes nothing (update runs with sudo and needs --confirm; stack updates are qdeploy stack update)
- qdeploy container list [--metrics], qdeploy container show <name>: show every container of the host, own stacks, foreign Compose projects and single containers, with state; changes nothing (run with sudo; see qdeploy help containers)
- qdeploy container logs <name>|--stack <stack> [--since T] [--until T] [--tail N] [--follow]: show the output of a container or of a whole stack (run with sudo; see qdeploy help containers)
- qdeploy container start|stop|restart <name> [--yes]: start, stop or restart one container of the host after a question (run with sudo; see qdeploy help containers)
- qdeploy version: print the version
- qdeploy help <topic>: show a help topic`

const credentialsTopic = `The service keeps its own secrets in a private store that only the service user can read. The commands need sudo because they talk to the service over its root-only channel.

Deploy keys:
- Every remote stack folder has its own key, because a git platform accepts a deploy key in one repository only. The first folder uses the key without --dir; every further one needs its own before qdeploy repo set binds it.
- qdeploy key generate [--dir <path>]: create an Ed25519 key in the service for the stack folder and print its public part and SHA256 fingerprint; the folder need not be bound yet
- qdeploy key generate --replace: replace an existing key; without --replace an existing key is kept and the command refuses
- qdeploy key show [--dir <path>]: print the public part again
- Enter the public part as a deploy key in the git platform of that folder's repository. The private key never leaves the service.

Credentials:
- qdeploy credential set <name>: store a registry token or notification credential; the value is read without echo at a terminal, or from standard input when piped, and never from an argument
- qdeploy credential list: show names and modification times, never values
- qdeploy credential delete <name>: remove a credential
- A name has 1 to 64 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter.
- Registry login for update checks: name the credential "registry." plus the registry host in lowercase with ':' written as '-' (Docker Hub: registry.docker.io), with the value <user>:<token>. Without such a credential the registry is asked anonymously.
- GitHub token for changelogs: name the credential "github.com", with the token alone as value. It is sent only to the GitHub API and raises the rate limit; without it GitHub is asked anonymously.
- A stored value is not shown again and is removed from the service's log output.`

const sourcesTopic = `A host can deploy from several stack folders, each a source with its own clone, engine state, poll and deploy key. Commands on a stack need no source: a stack is named by its Compose project, and the service finds its folder.

Sources:
- qdeploy repo set <ssh-url> --dir <path> binds a further folder next to those bound earlier; a bound folder is never rebound to another URL, and folders may not lie inside each other (see qdeploy help repository).
- The id of a source comes from its path and never changes: /opt/stacks becomes opt-stacks, /srv/services/stacks becomes srv-services-stacks.
- Each subfolder of a source with a compose file is a stack. When the root of the source holds a compose file, found as in a subfolder, the source is exactly one stack instead: the whole folder except .qatlas/, named by its Compose project, else by the folder's name. Its subfolders are then part of it, and those with a compose file of their own get the finding ignored-stack-folder.
- Every remote source has its own deploy key: qdeploy key generate --dir <path> before qdeploy repo set (see qdeploy help credentials).
- A source whose fetch or poll fails keeps its state; the other sources go on. qdeploy status shows each source with its last poll, error and commits, and each stack with its source.
- The maintenance window and qdeploy stack updates cover the stacks of every source.

Stack names:
- Compose tells stacks apart only by project name, so a name is deployed from one source alone. When two sources list the same name, the stack already deployed keeps it, else the one of the source bound first; the other gets the finding name-collision and is never deployed until one of them gets another project name.
- qdeploy status lists such a stack as source.<id>.stack.<name>; the stack that holds the name stays stack.<name>.

Earlier installations:
- A host bound to one repository by an earlier version keeps running: at its first start the service makes the binding its first source, moves the engine state below it and keeps the existing deploy key. Nothing needs to be done.`

const repositoryTopic = `The service clones the stacks repository of the host over SSH with its own deploy key and checks the server with its own known_hosts file; the system's SSH configuration and known hosts are not used. Host key checking is always strict.

Binding:
- qdeploy key generate, then enter the public key as a deploy key with write access in the git platform; qdeploy pushes its version and revert commits with it
- qdeploy repo set <ssh-url> --dir <path>: the URL is user@host:path or ssh://[user@]host[:port]/path; --dir names the stack folder, is required and has no default
- On first contact the host key and its SHA256 fingerprint are shown. At a terminal you confirm it by answering the question (default no). Without a terminal, pass the expected fingerprint: --fingerprint SHA256:...
- Compare the fingerprint with the one the git platform publishes before you confirm it.
- The stack folder is any absolute path, for example on another disk; each host chooses its own. It must be empty or already a clone of the same URL. Another folder becomes a further stack folder next to the earlier ones, with its own deploy key from qdeploy key generate --dir <path>; a bound folder is never rebound to another URL.
- A folder that does not exist is created only after you confirmed its exact path: answer the question at a terminal, or pass --create without one. It gets the owner qdeploy:qdeploy and mode 0755. Neither the installer, the service nor an update creates a stack folder.
- The service runs sandboxed and may write only where the systemd drop-in /etc/systemd/system/qdeploy.service.d/10-stack-folders.conf allows it. repo set adds the folder there as ReadWritePaths=-<path> (entries are only ever added), reloads systemd and restarts the service once. A bound folder without that entry, as after an update from an older release, is added when you next run qdeploy as root.
- The same steps are offered by sudo qdeploy in the Settings area.

Afterwards:
- qdeploy repo fetch: fetch new commits into the remote-tracking branch and report whether the working copy is behind; the working copy itself is not changed
- qdeploy stack list: list each stack with its image version variables, their values and its update mode (none without settings)
- A host key that differs from the pinned one aborts every command. The pinned key is never replaced by a command; verify the change with the git platform's operator first.`

const stackSettingsTopic = `The settings of a stack say how qdeploy updates, backs up and checks it. They live in the stacks repository as .qatlas/qdeploy/<stack>.yaml in its root, one file per stack, named by the stack's Compose project name. A stack without settings is deployed but never updated.

The settings are shown and changed only in the terminal interface or with qdeploy stack settings <stack> and qdeploy stack settings set <stack>; nobody edits the file by hand. qdeploy checks them against the stack, writes the file, commits it and pushes the commit. A settings commit does not touch the stack folder and starts no run; the settings act on the next update or deploy. The file is read as data only: unknown fields, a missing version, or a path that is absolute or leaves the stack folder are rejected.

Fields (all optional except version):
- version: 1
- compose.files: the compose files of the stack as a list of relative paths inside the stack folder, in merge order like COMPOSE_FILE; it takes precedence over COMPOSE_FILE in .env and over the default names
- update.mode: manual (default) or automatic
- update.ceiling: patch, minor (default) or major; the largest version step taken; versions are MAJOR.MINOR.PATCH (SemVer or CalVer), and with CalVer the first part, for example the year, counts as major
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

Image versions stand as variables in the stack's .env, for example image: registry.example.test/demo/app:${DEMO_APP_VERSION}. A fixed or floating tag such as latest leaves the image deploy only; qdeploy stack check names it (see qdeploy help stack-capabilities).`

const backupsTopic = `A backup saves the data of one stack while the stack is stopped, so the copy is consistent. What is saved is listed in the backup section of the stack settings (see qdeploy help stack-settings). Backups are kept in the service's state directory under backups/<stack>/<backup>, readable by the service user only.

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
- Every deployment run of a stack whose settings have a backup section makes a backup after it stopped the stack; when the run is rolled back, that backup is restored. These backups count for retention like any other.

Retention:
- backup.retention.keep and backup.retention.max_age_days apply after each successful backup; a backup is kept only while it satisfies every limit that is set. The newest backup is never removed.

Restore:
- The backup id comes from qdeploy stack backup list; the newest is never chosen for you.
- At a terminal the command asks first (default no). Without a terminal, and with --agent, it refuses unless --yes is given.
- Every archive is read through before the first target is emptied. Each target is then emptied and unpacked inside the helper container, on the mounted path only.
- A backup that holds a data directory or volume the settings no longer list is refused as a whole.
- The command output is not restored; the settings have no restore command.
- A restore that fails after the first target was emptied leaves the stack stopped, because it would run on incomplete data.
- A volume must exist before it is backed up or restored; deploy the stack first.`

const stackCapabilitiesTopic = `qdeploy stack check reads the working copy of the stacks repository and reports for each stack which functions of qdeploy work with its files, which do not, why, and what a person would change. qdeploy never changes these files itself. The TUI shows the same report at the end of the stack detail.

Functions, in this order:
- deploy: works when qdeploy can read the stack: its compose files exist and are safe, its settings are valid, secrets.env, if there is one, is a regular file of at most 1 MiB, and every image version variable has a value. An .env with lines qdeploy cannot parse still deploys, unless such a line is a COMPOSE_ setting, because Compose reads it itself; the stack is then deploy-only.
- update, with the mode automatic, manual or none: works when the stack deploys, qdeploy can read .env, at least one image takes its version from a variable in .env whose value has a MAJOR.MINOR.PATCH core (SemVer or CalVer, x.y.z), and the stack settings exist. Without settings the mode is none and updates are not switched on. An image with a fixed tag, a digest, no tag or a floating tag such as latest is deploy only and is left alone; a version that only comes from a default in the compose file, or a variable that is the tag of two different images, stops the update of the whole stack.
- rollback: the rollback with data. A failed run of a stack that was deployed before goes back to the previous commit; it restores the data only when the stack settings have a backup section, from the backup the run made before the change.
- backup: works when the stack settings have a backup section, every backup path lies below data/, and the stack name has lowercase letters, digits, - and _ only and at most 63 characters (see qdeploy help backups).
- redact: the values of secrets.env and of the .env keys whose name contains PASSWORD, SECRET, TOKEN or KEY are removed from the service log, the run logs and the notifications. It does not work for a secrets.env that is a link or too large, and finds nothing when the stack has neither.

Findings (rule ID, severity):
- missing-compose-file (error): a file named in compose.files or COMPOSE_FILE does not exist
- invalid-compose-file (error): a compose.files or COMPOSE_FILE entry is unsafe or not a regular file
- invalid-rules-file (error): the stack settings are invalid
- unknown-rules-variable (error): the stack settings name an image variable the stack does not have
- unreadable-stack (error, or warning for an .env qdeploy cannot parse): a file of the stack cannot be read
- unreadable-secrets-env (error): secrets.env is a link or too large
- unset-image-variable (error): an image version variable has no value, so the image has an empty tag
- tracked-secrets-env (error with a remote, else warning): git tracks secrets.env, so its secrets reach the remote
- duplicate-key (warning): a key stands in both .env and secrets.env; keep it in one file
- secrets-env-not-ignored (warning): .gitignore does not cover secrets.env
- tracked-data (warning): git tracks files below data/
- old-rules-file (warning): the stack folder holds a qdeploy.yaml, which is no longer read; set the stack settings instead and delete it
- ignored-stack-folder (warning): a subfolder holds a compose file but is part of the one stack whose compose file is in the root of the stack folder

A finding is an error only when the stack cannot be deployed or a secret would reach a remote; everything else is a warning or only part of the functions. The exit code is 1 when there is at least one error.

Layout of a stack folder:
- one or more compose files: the list compose.files in the stack settings, else COMPOSE_FILE in .env (entries separated by ':', or by COMPOSE_PATH_SEPARATOR), else the first of compose.yaml, compose.yml, docker-compose.yaml, docker-compose.yml
- .env (optional): non-secret values such as image versions, domains, ports, COMPOSE_FILE and COMPOSE_PROJECT_NAME
- secrets.env (optional): the secrets, set with qdeploy stack secret set or in the stack detail of the TUI; kept out of git with .gitignore
- data/ (optional): the data directories of the host, kept out of git

A stack is named by its Compose project name, which is not necessarily its folder: COMPOSE_PROJECT_NAME in .env, else the top-level name: of the compose files, else the folder name. This name is used by every command. When the project name of a deployed stack changes, both names get the finding project-renamed and nothing is deployed automatically: the old project keeps running and is not stopped. qdeploy stack deploy <new name> is the decision to start the new project next to the old one, with new volumes prefixed by the new name; the old project must be stopped by a person.

Secret values are never shown or logged; the check reads only the key names of secrets.env. Compose runs with --env-file for each of .env and secrets.env that exists.`

const dataDirectoriesTopic = `A data directory is a bind mount source inside the stack folder that git does not track, for example ./data/db. If it is missing when a container starts, Docker creates it owned by root, and an image that runs as another user then cannot write to it.

The finding data-dir-missing:
- Before a run touches the stack, the service reads the bind mount sources inside the stack folder from the rendered Compose configuration and finds the user each image runs as. A missing, unversioned directory of a service that does not run as root stops the run with this finding, which lists the paths relative to the stack folder and the owner (UID:GID) the image user suggests, when it is known.
- The finding clears and the same commit runs by itself as soon as all listed directories exist. A manual run (qdeploy stack deploy) checks again. Nothing is created without you.
- Directories of services that run as root, named volumes, files and tracked paths are not part of the finding.

Resolving it (run with sudo):
- In the terminal interface, open the stack detail, check the owner and mode, and confirm the creation.
- qdeploy stack datadir <stack> [--owner UID:GID] [--mode 0750] --yes creates the listed directories and then starts a run of the stack. Without --yes it asks at a terminal and otherwise only prints what it would create. The owner comes from --owner or from the finding; the mode is 0750 unless given.
- Existing directories are never changed, missing parents are created like the listed directories, and nothing outside the stack folder is touched. A symlink on the way refuses the creation.
- If you create the directories yourself, set the owner the container user needs; the service takes them as they are.`

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
- qdeploy container logs <name>: the output (stdout and stderr) of one container by name or ID; with --stack <stack> the merged output of all containers of an own stack, each line with its container. Not the protocol of a run: that is qdeploy stack log <run>
- qdeploy container start|stop|restart <name> [--yes]: act on one container by name or ID, also a foreign or single one

Kinds of a group:
- own: a Compose project that is a stack of the bound stacks repository and was started from that stack's folder
- foreign: any other Compose project; it shows its working directory and compose files
- single: a container that belongs to no Compose project
- conflict: a foreign project that has the name of a stack of this host but was started from another folder; it is not the stack and is marked in the list

Logs: --since and --until take a duration back from now (90s, 15m, 2h) or an RFC 3339 time; --tail shows the last n lines (default 100, at most 5000, 0 for all); --follow asks about once a second for new lines until Ctrl-C (exit code 0). Secret values of the stacks are masked and control characters replaced; --output json prints one object per line. The service answers in pages and the command continues at the time of the last line, so no line is repeated or missed; lines with the same time are never split between pages.

State of a group is the worst state of its containers, in this order: unhealthy, restarting, stopped, starting, ok. The uptime of a group is that of its youngest running container; restarts are summed.

Actions: the command names the container and its kind and asks first (default no); --yes answers the question, and without a terminal --yes is required. In the terminal interface (qdeploy tui), S, X and Z in the area Containers do the same for the container on the cursor or in the open detail, after a question that names the container and its kind (default cancel) and act by the ID the container has at that moment; they do nothing on a group header. The result shows the state afterwards, and a container that is not ready counts as failed. A stop signals the container and kills it after ten seconds; stopping a stopped or starting a running container changes nothing. After a start or restart the service waits up to a minute until the container runs and, if it has a health check, is healthy; if it is not ready by then the command shows the state and fails. A container of a stack of this host is refused while a run of that stack is active.

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
