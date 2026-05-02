package reconcile

import (
	"path/filepath"
	"strings"
)

func companionMoves(oldMediaPath string, newMediaPath string, companions []CompanionRecord) []FileMove {
	oldBase := strings.TrimSuffix(filepath.Base(oldMediaPath), filepath.Ext(oldMediaPath))
	newBase := strings.TrimSuffix(filepath.Base(newMediaPath), filepath.Ext(newMediaPath))
	dir := filepath.Dir(newMediaPath)
	if dir == "." {
		dir = ""
	}
	moves := make([]FileMove, 0, len(companions))
	for _, companion := range companions {
		target := filepath.ToSlash(filepath.Join(dir, newBase+companionSuffix(filepath.Base(companion.Path), oldBase)))
		if target != companion.Path {
			moves = append(moves, FileMove{From: companion.Path, To: target})
		}
	}
	return moves
}

func companionSuffix(filename string, oldMediaBase string) string {
	if strings.HasPrefix(filename, oldMediaBase+".") {
		return strings.TrimPrefix(filename, oldMediaBase)
	}
	extension := compoundExtension(filename)
	if extension == "" {
		return filepath.Ext(filename)
	}
	return extension
}

func compoundExtension(filename string) string {
	name := filepath.Base(filename)
	index := strings.Index(name, ".")
	if index < 0 {
		return ""
	}
	return name[index:]
}
