# qdeploy

qdeploy (Qatlas Deploy) runs as a service on a Linux host and keeps the Docker stacks of that host rolled out
and up to date from its stacks repository.

## Installation

On Ubuntu or Debian with systemd and Docker, as root or with sudo:

```sh
curl -fsSL https://github.com/castrowithcee/qatlas-deploy/releases/latest/download/install.sh | sudo sh
```

The installer verifies the signature of the release checksums, installs `qdeploy` as the systemd service
`qdeploy` running as the system user `qdeploy` (group `docker`), and starts it. Running it again updates the
installation. It needs `curl`, `ssh-keygen` and the group `docker`.

The service writes only `/var/lib/qdeploy` and the stacks directory `/opt/stacks`. Create the stacks
directory before the service needs it, then restart the service once:

```sh
sudo install -d -o qdeploy -g qdeploy -m 0755 /opt/stacks
sudo systemctl restart qdeploy
```

Then open `sudo qdeploy tui` or run `sudo qdeploy key generate` and `sudo qdeploy repo set <ssh-url>`.

To remove the program, unit and manpages (configuration, state, user and group stay, and the script lists
the commands that remove them):

```sh
curl -fsSL https://github.com/castrowithcee/qatlas-deploy/releases/latest/download/install.sh | sudo sh -s -- --uninstall
```

## Building

Go 1.26 or newer is required.

```sh
go build -o qdeploy .
```

## License

GNU Affero General Public License, version 3. See [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
