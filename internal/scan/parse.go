package scan

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nssteinbrenner/anitogo"
)

type parsedEpisodeRef struct {
	Season int
	Number int
}

type filenameParsingStrategy func(filename string) (parsedEpisodeRef, bool, error)

var filenameParsingStrategies = []filenameParsingStrategy{
	parseEpisodeRefWithRegex,
	parseEpisodeRefWithAnitogo,
}

var (
	seasonEpisodePattern = regexp.MustCompile(`(?i)\bS([0-9]{1,2})E([0-9]{1,3})\b`)
	episodeMarkerPattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])E([0-9]{1,3})(?:[^[:alnum:]]|$)`)
	seasonDirPattern     = regexp.MustCompile(`(?i)^Season[[:space:]]+([0-9]+)$`)
)

func parseSeasonDir(name string) (int, bool) {
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

func InferEpisodeFromFilename(name string) (int, int, bool) {
	return inferEpisodeFromFilename(name)
}

func inferEpisodeFromFilename(name string) (int, int, bool) {
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

func parseEpisodeRefWithRegex(filename string) (parsedEpisodeRef, bool, error) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	matches := seasonEpisodePattern.FindStringSubmatch(base)
	if len(matches) == 3 {
		season, seasonErr := strconv.Atoi(matches[1])
		episode, episodeErr := strconv.Atoi(matches[2])
		if seasonErr == nil && episodeErr == nil && episode > 0 {
			return parsedEpisodeRef{Season: season, Number: episode}, true, nil
		}
	}
	matches = episodeMarkerPattern.FindStringSubmatch(base)
	if len(matches) == 2 {
		episode, err := strconv.Atoi(matches[1])
		if err == nil && episode > 0 {
			return parsedEpisodeRef{Season: -1, Number: episode}, true, nil
		}
	}
	return parsedEpisodeRef{}, false, nil
}

func parseEpisodeRefWithAnitogo(filename string) (parsedEpisodeRef, bool, error) {
	parsed := anitogo.Parse(filename, anitogo.DefaultOptions)
	if parsed == nil || len(parsed.EpisodeNumber) != 1 {
		return parsedEpisodeRef{}, false, nil
	}
	episode, ok := parsePositiveInt(parsed.EpisodeNumber[0])
	if !ok {
		return parsedEpisodeRef{}, false, nil
	}
	season := -1
	if len(parsed.AnimeSeason) > 0 {
		if len(parsed.AnimeSeason) != 1 {
			return parsedEpisodeRef{}, false, nil
		}
		parsedSeason, ok := parsePositiveInt(parsed.AnimeSeason[0])
		if !ok {
			return parsedEpisodeRef{}, false, nil
		}
		season = parsedSeason
	}
	return parsedEpisodeRef{Season: season, Number: episode}, true, nil
}

func parsePositiveInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return 0, false
	}
	return parsed, true
}
