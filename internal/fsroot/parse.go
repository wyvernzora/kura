package fsroot

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nssteinbrenner/anitogo"
)

type ParsedEpisodeRef struct {
	Season int
	Number int
}

type FilenameParsingStrategy func(filename string) (ParsedEpisodeRef, bool, error)

var filenameParsingStrategies = []FilenameParsingStrategy{
	parseEpisodeRefWithRegex,
	parseEpisodeRefWithAnitogo,
}

var (
	seasonEpisodePattern = regexp.MustCompile(`(?i)\bS([0-9]{1,2})E([0-9]{1,3})\b`)
	episodeMarkerPattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])E([0-9]{1,3})(?:[^[:alnum:]]|$)`)
	seasonDirPattern     = regexp.MustCompile(`(?i)^Season[[:space:]]+([0-9]+)$`)
	mediaFactsPattern    = regexp.MustCompile(`\(([^()]*)\)\.[^.]+$`)
)

func ParseSeasonDir(name string) (int, bool) {
	matches := seasonDirPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return 0, false
	}
	season, err := strconv.Atoi(matches[1])
	if err != nil || season < 0 {
		return 0, false
	}
	return season, true
}

func RecognizedVideoFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return slices.Contains([]string{
		".mkv",
		".mp4",
		".m4v",
		".avi",
		".mov",
		".webm",
		".ts",
		".m2ts",
		".wmv",
		".ogm",
		".ogv",
	}, extension)
}

func InferSourceFromFilename(path string) string {
	name := filepath.ToSlash(path)
	matches := mediaFactsPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "unknown"
	}
	fields := strings.Fields(matches[1])
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[0]
}

func InferEpisodeFromFilename(name string) (int, int, bool) {
	for _, strategy := range filenameParsingStrategies {
		ref, ok, err := strategy(name)
		if err != nil {
			continue
		}
		if ok {
			return ref.Season, ref.Number, true
		}
	}
	return 0, 0, false
}

func parseEpisodeRefWithRegex(filename string) (ParsedEpisodeRef, bool, error) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	matches := seasonEpisodePattern.FindStringSubmatch(base)
	if len(matches) == 3 {
		season, seasonErr := strconv.Atoi(matches[1])
		episode, episodeErr := strconv.Atoi(matches[2])
		if seasonErr == nil && episodeErr == nil && episode > 0 {
			return ParsedEpisodeRef{Season: season, Number: episode}, true, nil
		}
	}
	matches = episodeMarkerPattern.FindStringSubmatch(base)
	if len(matches) == 2 {
		episode, err := strconv.Atoi(matches[1])
		if err == nil && episode > 0 {
			return ParsedEpisodeRef{Season: -1, Number: episode}, true, nil
		}
	}
	return ParsedEpisodeRef{}, false, nil
}

func parseEpisodeRefWithAnitogo(filename string) (ParsedEpisodeRef, bool, error) {
	parsed := anitogo.Parse(filename, anitogo.DefaultOptions)
	if parsed == nil || len(parsed.EpisodeNumber) != 1 {
		return ParsedEpisodeRef{}, false, nil
	}
	episode, ok := parsePositiveInt(parsed.EpisodeNumber[0])
	if !ok {
		return ParsedEpisodeRef{}, false, nil
	}
	season := -1
	if len(parsed.AnimeSeason) > 0 {
		if len(parsed.AnimeSeason) != 1 {
			return ParsedEpisodeRef{}, false, nil
		}
		parsedSeason, ok := parsePositiveInt(parsed.AnimeSeason[0])
		if !ok {
			return ParsedEpisodeRef{}, false, nil
		}
		season = parsedSeason
	}
	return ParsedEpisodeRef{Season: season, Number: episode}, true, nil
}

func parsePositiveInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return 0, false
	}
	return parsed, true
}
