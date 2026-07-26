# tape-backup

`tape-backup` is Kura's peer service for operator-assisted LTO archival. It
owns the tape catalog and, in later slices, will own plans, allocation,
status, and tape execution. See the
[backup design](../../scratch/lto-backup-design.md#deployment) for the
deployment boundary.

The `kura-tape-backup` binary has two entrypoints:

- `kura-tape-backup serve` is the long-lived control plane. The scaffold
  loads configuration, writes a structured startup log, and exits with
  `serve is not implemented`.
- `kura-tape-backup run` is the ephemeral tape-drive executor. The scaffold
  loads the same configuration, writes a structured startup log, and exits
  with `run is not implemented`.

Both entrypoints read `/etc/kura/tape-backup.toml` by default. The
library-manager bearer token is supplied separately through `KURA_TOKEN`.
See [`config.example.toml`](config.example.toml) for the file shape.

Run the local checks with:

```sh
make check
```
