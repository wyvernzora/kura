package inbox

import "strings"

// hiddenSuffixes lists filename suffixes treated as hidden by default
// (skipped from listings unless IncludeHidden=true). Covers the common
// download-in-flight markers from popular clients.
var hiddenSuffixes = []string{
	".partial",    // generic partial download
	".crdownload", // Chrome
	".!qB",        // qBittorrent
	".aria2",      // aria2c
	".tmp",        // generic
}

// isHidden reports whether a NFC-normalized filename should be omitted
// from default listings. Dotfiles are always hidden; suffix-matched
// names are treated as in-flight downloads.
func isHidden(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	for _, suffix := range hiddenSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
