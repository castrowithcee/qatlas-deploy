# Third-party notices

## qatlas-sysmin

Parts of this repository are copied from or derived from qatlas-sysmin
(https://github.com/castrowithcee/qatlas-sysmin):

- the command and error handling structure of `internal/cli`, the signal handling of its `run` command and the help topic package `internal/helptopics`
- the protocol frame, size and time limits and the connection handling of `internal/control` and `internal/service`, and the request/answer calls of `internal/client`, reduced to a root-only channel without groups, vault or approvals
- `.github/workflows/ci.yml`

Copyright (c) 2026 castrowithcee

qatlas-sysmin is licensed under the GNU Affero General Public License, version 3. The full license text is in
[LICENSE](LICENSE).
