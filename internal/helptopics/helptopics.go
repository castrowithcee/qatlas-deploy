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
		{Name: "stack-standard", Short: "The stacks-repo standard that qdeploy check enforces", Text: stackStandardTopic},
		{Name: "stack-rules", Short: "The qdeploy.yaml rule file of a stack", Text: stackRulesTopic},
		{Name: "backups", Short: "Backing up and restoring the data of a stack", Text: backupsTopic},
	}
}

const overview = `qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and up to date from its stacks repository.

Commands:
- qdeploy run: run the service in the foreground until it receives SIGTERM or SIGINT
- qdeploy status: show whether the service is running, its version and start time, the last poll and the state of each stack (run with sudo)
- qdeploy key generate|show: create the SSH deploy key and show its public part (run with sudo)
- qdeploy repo set <ssh-url> [--dir <path>]: bind the stacks repository, confirm its host key and clone it (run with sudo)
- qdeploy repo fetch: fetch new commits without touching the working copy (run with sudo)
- qdeploy stacks: list the stacks of the working copy with image versions and update mode (run with sudo)
- qdeploy check [stack]: check the stacks of the working copy against the stacks-repo standard; exit code 1 on errors (run with sudo)
- qdeploy deploy <stack>: start a manual run of a stack at the commit the working copy has (run with sudo)
- qdeploy runs [stack]: list the recorded runs, newest first (run with sudo)
- qdeploy log <run>: print the log of a run (run with sudo)
- qdeploy resume <stack>: clear the pause a failed rollback left on a stack; it starts no run (run with sudo)
- qdeploy updates [stack] [--refresh]: show the current and the available image versions per stack and variable, with newer major versions flagged as not automatic; --refresh asks the registries now (run with sudo)
- qdeploy backup create|list <stack>, qdeploy backup restore <stack> <backup> [--yes]: back up, list and restore the data of a stack (run with sudo; see qdeploy help backups)
- qdeploy credential set|list|delete: manage registry tokens and notification credentials (run with sudo)
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
- A stored value is not shown again and is removed from the service's log output.`

const repositoryTopic = `The service clones the stacks repository of the host over SSH with its own deploy key and checks the server with its own known_hosts file; the system's SSH configuration and known hosts are not used. Host key checking is always strict.

Binding:
- qdeploy key generate, then enter the public key as a read-only deploy key in the git platform
- qdeploy repo set <ssh-url> [--dir <path>]: the URL is user@host:path or ssh://[user@]host[:port]/path; the directory defaults to /opt/stacks and must be absolute
- On first contact the host key and its SHA256 fingerprint are shown. At a terminal you confirm it by answering the question (default no). Without a terminal, pass the expected fingerprint: --fingerprint SHA256:...
- Compare the fingerprint with the one the git platform publishes before you confirm it.
- The directory must not exist, be empty, or already be a clone of the same URL. A repository bound earlier is not replaced.

Afterwards:
- qdeploy repo fetch: fetch new commits into the remote-tracking branch and report whether the working copy is behind; the working copy itself is not changed
- qdeploy stacks: list each stack with its image version variables, their values and its update mode (none without a qdeploy.yaml)
- A host key that differs from the pinned one aborts every command. The pinned key is never replaced by a command; verify the change with the git platform's operator first.`

const stackRulesTopic = `Each stack folder of the stacks repository may hold a qdeploy.yaml with the rules for updating it. Without the file the stack is deployed but never updated. The file is read as data only: unknown fields, a missing version, or a path that is absolute or leaves the stack folder are rejected.

Fields (all optional except version):
- version: 1
- update.mode: manual (default) or automatic
- update.ceiling: patch, minor (default) or major; the largest version step taken
- update.major: stepwise (default, one major version at a time) or direct
- update.tags.<VARIABLE>: regular expression a candidate tag must match; VARIABLE is the image version variable in .env
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

Image versions stand as variables in the stack's .env, for example image: registry.example.test/demo/app:${DEMO_APP_VERSION}. qdeploy check reports fixed and floating tags such as latest (see qdeploy help stack-standard).`

const backupsTopic = `A backup saves the data of one stack while the stack is stopped, so the copy is consistent. What is saved is listed in the backup section of the stack's qdeploy.yaml (see qdeploy help stack-rules). Backups are kept in the service's state directory under backups/<stack>/<backup>, readable by the service user only.

Commands (run with sudo; the service must run):
- qdeploy backup create <stack>: stop the stack if it runs, save every listed data directory and volume, run the backup command, start the stack again, then remove backups beyond the retention limits
- qdeploy backup list <stack>: the backups of a stack, newest first, with time, deployed commit, size and number of parts
- qdeploy backup restore <stack> <backup> [--yes]: replace the data with the content of one backup; the stack is stopped and started again if it ran

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
- The backup id comes from qdeploy backup list; the newest is never chosen for you.
- At a terminal the command asks first (default no). Without a terminal, and with --agent, it refuses unless --yes is given.
- Every archive is read through before the first target is emptied. Each target is then emptied and unpacked inside the helper container, on the mounted path only.
- A backup that holds a data directory or volume the rules no longer list is refused as a whole.
- The command output is not restored; qdeploy.yaml has no restore command.
- A restore that fails after the first target was emptied leaves the stack stopped, because it would run on incomplete data.
- A volume must exist before it is backed up or restored; deploy the stack first.`

const stackStandardTopic = `qdeploy check compares the working copy of the stacks repository with this standard. It reports each deviation as a finding with stack, rule, severity, cause and fix. Errors break or endanger a deploy or leak secrets; warnings leave the stack working. The exit code is 1 when there is at least one error.

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

Secret values are never read or shown; only the key names of secrets.env are compared with .env.`
