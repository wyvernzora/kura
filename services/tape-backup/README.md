# tape-backup

`tape-backup` is Kura's peer service for operator-assisted LTO archival. It
owns the tape catalog, plans, allocation, status, and tape execution. See the
[backup design](../../scratch/lto-backup-design.md#deployment) for the
deployment boundary.

The `kura-tape-backup` binary has one entrypoint:

- `kura-tape-backup serve` hosts the authenticated REST control plane and runs
  tape sessions in-process. The real hardware-drive constructor remains
  deferred to the hardware slice, so production startup currently stops at
  that explicit boundary.

The entrypoint reads `/etc/kura/tape-backup.toml` by default. The suite bearer
token is supplied separately through `KURA_TOKEN`.
See [`config.example.toml`](config.example.toml) for the file shape.

Run the local checks with:

```sh
make check
```
