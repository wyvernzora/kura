List entries under the inbox root (where new media drops before being staged into the library). Returns plain text optimized for agent reading: one line per entry, columns `[kind size mtime path]`.

Use this tool to discover what's available to stage. The `selector` field on each file (encoded as `inbox:<rel>`) is the value to pass back to `kura_stage`.

Defaults:
- `recursive` = false (one-level listing).
- `limit` = 500. When exceeded, output is truncated and a hint footer suggests narrowing.
- Hidden files (dotfiles, `*.partial`, `*.crdownload`, `*.!qB`) are skipped unless `includeHidden` = true.

Sort: mtime descending (newest first), name ascending tiebreak.

<!-- schema-note
Parameter schema is defined in tool_inbox_list.go (jsonschema tags on inboxListInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `path` (string, optional) — subpath relative to the inbox root (forward-slash, no leading slash). Empty lists the root.
- `recursive` (bool, optional) — when true, walks subdirectories up to `depth` levels deep.
- `depth` (int, optional) — recursive depth cap (default 3, max 5). Ignored when `recursive=false`.
- `limit` (int, optional) — cap on entries returned (default 500, max 5000). Truncation surfaces in the trailing footer.
- `kind` (string, optional) — kind filter: `file`, `dir`, or `symlink`.
- `nameGlob` (string, optional) — `filepath.Match`-style basename glob (e.g. `*.mkv`).
- `includeHidden` (bool, optional) — when true, surfaces dotfiles and download-in-flight markers (`.partial`, `.crdownload`, etc.).
<!-- /schema -->

## Usage examples

```
kura_inbox_list                              # one-level listing of inbox root
kura_inbox_list(path="[BDrip] Show")        # children of one release dir
kura_inbox_list(recursive=true, depth=3)    # walk subtrees, up to depth 5
kura_inbox_list(nameGlob="*.mkv")           # filter by basename pattern
kura_inbox_list(kind="file")                # only files (or "dir" / "symlink")
```

Output `path` column is what you wrap as `inbox:<path>` to pass to `kura_stage`. If the response is truncated, the footer suggests narrowing via `path`, `nameGlob`, or raising `limit` (up to 5000).
