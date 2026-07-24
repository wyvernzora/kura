package api

// AliasEntry is one title or alias entry from an external metadata source,
// paired with its language tag. Lang is BCP-47 base form (e.g. "ja",
// "zh-TW"); empty string is permitted — TVDB aliases are frequently
// untagged.
type AliasEntry struct {
	Lang  string `json:"lang"`
	Alias string `json:"alias"`
}

// SeriesAliases is the response shape for kura_aliases: all known titles
// and aliases for a series as reported by the metadata provider.
type SeriesAliases struct {
	Ref     string       `json:"ref"`
	Aliases []AliasEntry `json:"aliases"`
}
