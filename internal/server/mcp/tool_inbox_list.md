List entries under the inbox root (where new media drops before being staged into the library). Returns structured `path`, `entries`, truncation, and hint fields.

Use this tool to discover what's available to stage. Each output `path` value is an `inbox:<rel>` selector to pass back to `kura_stage`.

Defaults:
- `recursive` = false (one-level listing).
- `limit` = 500. When exceeded, `truncated`, `elidedCount`, and `hint` suggest narrowing.
- Hidden files (dotfiles, `*.partial`, `*.crdownload`, `*.!qB`) are skipped unless `includeHidden` = true.

Sort: mtime descending (newest first), name ascending tiebreak.

<!-- schema-note
Parameter schema is defined in tool_inbox_list.go (jsonschema tags on inboxListInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `path` (string, optional) — path relative to the inbox root (forward-slash, no leading slash). A directory lists its children; a file returns that exact entry. Empty lists the root.
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
kura_inbox_list(path="[BDrip] Show/E01.mkv") # inspect one exact entry
kura_inbox_list(recursive=true, depth=3)    # walk subtrees, up to depth 5
kura_inbox_list(nameGlob="*.mkv")           # filter by basename pattern
kura_inbox_list(kind="file")                # only files (or "dir" / "symlink")
```

Output `path` values are already `inbox:` selectors; pass them to `kura_stage` verbatim. If the response is truncated, narrow via `path`, `nameGlob`, or raise `limit` (up to 5000).
