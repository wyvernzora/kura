package selector

// Selector is a collection of terms used to identify a series.
type Selector struct {
	Terms []Term
}

func ParseSelector(raw []string) Selector {
	terms := make([]Term, 0, len(raw))
	for _, value := range raw {
		term := ParseTerm(value)
		if term == "" {
			continue
		}
		terms = append(terms, term)
	}
	return Selector{Terms: terms}
}
