package store

import "fmt"

// DuplicateEpisodeNumberError is returned when a series document declares the
// same episode number twice within one season.
type DuplicateEpisodeNumberError struct {
	Season  int
	Episode int
}

func (err DuplicateEpisodeNumberError) Error() string {
	return fmt.Sprintf("duplicate episode number S%02dE%02d", err.Season, err.Episode)
}
