# Third-party notices

## qatlas-sysmin

Parts of this repository are copied from or derived from qatlas-sysmin
(https://github.com/castrowithcee/qatlas-sysmin):

- the command and error handling structure of `internal/cli`, the signal handling of its `run` command and the help topic package `internal/helptopics`
- the protocol frame, size and time limits and the connection handling of `internal/control` and `internal/service`, and the request/answer calls of `internal/client`, reduced to a root-only channel without groups, vault or approvals
- the hardened git environment, SSH options, output limits and process-group handling of `internal/gitrepo`
- the poll loop with spread and wake-up, the per-stack run lock with dropped parallel triggers, and the state and run-log layout of `internal/engine`, reduced to a deployment engine without approvals, vault or step plans
- the runner with process-group handling and bounded output, the Compose command assembly and the container and HTTP checks of `internal/compose`, reduced to one fixed pull, up and check flow without step plans
- `.github/workflows/ci.yml`
- the end-to-end environment in `e2e/`: `run.sh`, `host.Dockerfile`, `registry.Dockerfile`, `fakes.Dockerfile` and the helper services in `e2e/fakes` (releases fake, webhook receiver, demo application)

Copyright (c) 2026 castrowithcee

qatlas-sysmin is licensed under the GNU Affero General Public License, version 3. The full license text is in
[LICENSE](LICENSE).
