# Third-party notices

## qatlas-sysmin

Parts of this repository are copied from or derived from qatlas-sysmin
(https://github.com/castrowithcee/qatlas-sysmin):

- the command and error handling structure of `internal/cli`, the signal handling of its `run` command and the help topic package `internal/helptopics`
- `.github/workflows/ci.yml`
- the end-to-end environment in `e2e/`: `run.sh`, `host.Dockerfile`, `registry.Dockerfile`, `fakes.Dockerfile` and the helper services in `e2e/fakes` (releases fake, webhook receiver, demo application)

Copyright (c) 2026 castrowithcee

qatlas-sysmin is licensed under the GNU Affero General Public License, version 3. The full license text is in
[LICENSE](LICENSE).
