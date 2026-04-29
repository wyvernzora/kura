package tvdb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
)

type statusRecord struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	RecordType  string `json:"recordType"`
	KeepUpdated bool   `json:"keepUpdated"`
}

type genreRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type translations struct {
	NameTranslations     []translation `json:"nameTranslations"`
	OverviewTranslations []translation `json:"overviewTranslations"`
}

type translation struct {
	Language string `json:"language"`
	Name     string `json:"name"`
	Title    string `json:"title"`
}

type remoteID struct {
	ID         tvdbString `json:"id"`
	Type       int        `json:"type"`
	SourceName string     `json:"sourceName"`
}

type links struct {
	Prev     tvdbString `json:"prev"`
	Self     tvdbString `json:"self"`
	Next     tvdbString `json:"next"`
	Total    int        `json:"total_items"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

func (l links) hasNext() bool {
	return l.Next.String() != ""
}

type tvdbString string

// UnmarshalJSON accepts either JSON strings or numbers for TVDB identifiers.
// Different TVDB endpoints expose the same conceptual IDs with different JSON
// scalar types.
func (s *tvdbString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}

	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		*s = tvdbString(stringValue)
		return nil
	}

	var numberValue json.Number
	if err := json.Unmarshal(data, &numberValue); err == nil {
		*s = tvdbString(numberValue.String())
		return nil
	}

	return fmt.Errorf("tvdb: expected string or number, got %s", string(data))
}

func (s tvdbString) String() string {
	return string(s)
}

func normalizeStatus(status string) metadata.SeriesStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "continuing", "ongoing", "returning series":
		return metadata.SeriesStatusContinuing
	case "ended", "completed":
		return metadata.SeriesStatusEnded
	case "upcoming", "in development", "planned":
		return metadata.SeriesStatusUpcoming
	default:
		return metadata.SeriesStatusUnknown
	}
}

func normalizeDate(value string) string {
	// TVDB date fields used by Kura are calendar dates, not instants.
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

func firstNormalizedDate(values ...string) string {
	for _, value := range values {
		if normalized := normalizeDate(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func yearFromDate(value string) int {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed.Year()
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func positiveIntPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
