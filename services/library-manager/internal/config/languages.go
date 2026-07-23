package config

import (
	"fmt"
	"strings"

	"golang.org/x/text/language"
)

// PreferredLanguages is the ordered language preference list used for
// provider title selection.
type PreferredLanguages struct {
	tags []language.Tag
}

func ParsePreferredLanguages(value string) (PreferredLanguages, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	tags := make([]language.Tag, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if strings.Contains(field, "_") {
			return PreferredLanguages{}, fmt.Errorf("invalid preferred language %q: use BCP-47 hyphen form", field)
		}
		tag, err := language.Parse(field)
		if err != nil {
			return PreferredLanguages{}, fmt.Errorf("invalid preferred language %q: %w", field, err)
		}
		normalized := tag.String()
		if seen[normalized] {
			continue
		}
		tags = append(tags, tag)
		seen[normalized] = true
	}
	return PreferredLanguages{tags: tags}, nil
}

func (p PreferredLanguages) IsEmpty() bool {
	return len(p.tags) == 0
}

func (p PreferredLanguages) Tags() []string {
	out := make([]string, 0, len(p.tags))
	for _, tag := range p.tags {
		out = append(out, tag.String())
	}
	return out
}
