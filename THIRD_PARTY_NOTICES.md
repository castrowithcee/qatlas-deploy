# Third-party notices

## qatlas-sysmin

Parts of this repository are copied from or derived from qatlas-sysmin
(https://github.com/castrowithcee/qatlas-sysmin):

- the command and error handling structure of `internal/cli`, the signal handling of its `run` command and the help topic package `internal/helptopics`
- the protocol frame, size and time limits and the connection handling of `internal/control` and `internal/service`, and the request/answer calls of `internal/client`, reduced to a root-only channel without groups, vault or approvals
- the terminal check before the terminal interface starts in `internal/cli` and the colour profile mapping of `internal/tui`
- the hardened git environment, SSH options, output limits and process-group handling of `internal/gitrepo`
- the poll loop with spread and wake-up, the per-stack run lock with dropped parallel triggers, and the state and run-log layout of `internal/engine`, reduced to a deployment engine without approvals, vault or step plans
- the runner with process-group handling and bounded output, the Compose command assembly and the container and HTTP checks of `internal/compose`, reduced to one fixed pull, up and check flow without step plans
- the volume archive through a helper container without network, the backup directory naming, the retention and the restore flow of `internal/backup`, rebuilt around Compose stacks (the backup store, metadata, data-directory handling and confirmation are new)
- the OCI registry client with its reference parsing and fake registry in `internal/registry`, and the version selection of `internal/version`, rebuilt on the shared HTTP and rate-limit packages with redirects refused, and reshaped to a per-variable ceiling, filter and major hint
- the line-based value editing and quoting rules of `internal/envfile`, rebuilt to work on bytes in memory and to follow the `.env` parsing of `internal/stacks`
- the GitHub release notes reader in `internal/changelog` (release list cache, text sanitizing and source address parsing), rebuilt on the shared HTTP and rate-limit packages with redirects refused, and reshaped to a result that always carries the releases page link
- `.github/workflows/ci.yml`
- the service unit `packaging/qdeploy.service` and its unit test in `packaging`, adapted to the unit and test of qsysmin
- the end-to-end environment in `e2e/`: `run.sh`, `host.Dockerfile`, `registry.Dockerfile`, `fakes.Dockerfile` and the helper services in `e2e/fakes` (releases fake, webhook receiver, demo application)

Copyright (c) 2026 castrowithcee

qatlas-sysmin is licensed under the GNU Affero General Public License, version 3. The full license text is in
[LICENSE](LICENSE).
