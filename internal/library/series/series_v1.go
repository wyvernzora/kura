package series

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed series_v1.schema.json
var seriesV1SchemaJSON []byte

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

type seriesV1 struct {
	SchemaVersion     int        `json:"schemaVersion"`
	ID                string     `json:"id"`
	ProviderRefs      []string   `json:"providerRefs"`
	PreferredProvider string     `json:"preferredProvider"`
	PreferredTitle    string     `json:"preferredTitle"`
	CanonicalTitle    string     `json:"canonicalTitle"`
	FilesystemTitle   string     `json:"filesystemTitle,omitempty"`
	LastScanned       string     `json:"lastScanned,omitempty"`
	Notes             string     `json:"notes,omitempty"`
	Seasons           []seasonV1 `json:"seasons,omitempty"`
	Specials          *seasonV1  `json:"specials,omitempty"`
}

type seasonV1 struct {
	Number   int         `json:"number"`
	Notes    string      `json:"notes,omitempty"`
	Episodes []episodeV1 `json:"episodes,omitempty"`
}

type episodeV1 struct {
	Number     int               `json:"number"`
	Media      mediaFileV1       `json:"media"`
	Companions []companionFileV1 `json:"companions"`
}

type mediaFileV1 struct {
	Path       string       `json:"path"`
	Source     string       `json:"source"`
	Size       int64        `json:"size"`
	MTime      string       `json:"mtime"`
	SampleHash string       `json:"sampleHash,omitempty"`
	MediaInfo  *mediaInfoV1 `json:"mediainfo,omitempty"`
}

type companionFileV1 struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type mediaInfoV1 struct {
	VideoCodec   string `json:"videoCodec,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	AudioCodec   string `json:"audioCodec,omitempty"`
	HasSubtitles bool   `json:"hasSubtitles"`
}

type DuplicateEpisodeNumberError struct {
	Season  int
	Episode int
}

func (err DuplicateEpisodeNumberError) Error() string {
	return fmt.Sprintf("duplicate episode number S%02dE%02d", err.Season, err.Episode)
}

func decodeSeries(data []byte, path string) (Series, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Series{}, fmt.Errorf("library: decode %s: %w", path, err)
	}

	switch header.SchemaVersion {
	case SeriesSchemaVersion:
		return decodeSeriesV1(data, path)
	default:
		return Series{}, fmt.Errorf("library: unsupported series schemaVersion %d", header.SchemaVersion)
	}
}

func encodeSeries(w io.Writer, series Series) error {
	if series.SchemaVersion != SeriesSchemaVersion {
		return fmt.Errorf("library: unsupported series schemaVersion %d", series.SchemaVersion)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(seriesToV1(series))
}

func decodeSeriesV1(data []byte, path string) (Series, error) {
	if err := validateSeriesV1JSON(data); err != nil {
		return Series{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var disk seriesV1
	if err := json.Unmarshal(data, &disk); err != nil {
		return Series{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	if err := validateUniqueEpisodeNumbers(disk); err != nil {
		return Series{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	return seriesFromV1(disk), nil
}

func seriesToV1(series Series) seriesV1 {
	return seriesV1{
		SchemaVersion:     SeriesSchemaVersion,
		ID:                series.ID,
		ProviderRefs:      cloneStrings(series.ProviderRefs),
		PreferredProvider: series.PreferredProvider,
		PreferredTitle:    series.PreferredTitle,
		CanonicalTitle:    series.CanonicalTitle,
		FilesystemTitle:   series.FilesystemTitle,
		LastScanned:       series.LastScanned,
		Notes:             series.Notes,
		Seasons:           seasonsToV1(series.Seasons),
		Specials:          seasonToV1Ptr(0, series.Specials),
	}
}

func seriesFromV1(disk seriesV1) Series {
	return Series{
		SchemaVersion:     disk.SchemaVersion,
		ID:                disk.ID,
		ProviderRefs:      cloneStrings(disk.ProviderRefs),
		PreferredProvider: disk.PreferredProvider,
		PreferredTitle:    disk.PreferredTitle,
		CanonicalTitle:    disk.CanonicalTitle,
		FilesystemTitle:   disk.FilesystemTitle,
		LastScanned:       disk.LastScanned,
		Notes:             disk.Notes,
		Seasons:           seasonsFromV1(disk.Seasons),
		Specials:          seasonFromV1Ptr(disk.Specials),
	}
}

func validateUniqueEpisodeNumbers(series seriesV1) error {
	for _, season := range series.Seasons {
		if err := validateUniqueEpisodeNumbersForSeason(season); err != nil {
			return err
		}
	}
	if series.Specials != nil {
		return validateUniqueEpisodeNumbersForSeason(*series.Specials)
	}
	return nil
}

func validateUniqueEpisodeNumbersForSeason(season seasonV1) error {
	seen := map[int]struct{}{}
	for _, episode := range season.Episodes {
		if _, exists := seen[episode.Number]; exists {
			return DuplicateEpisodeNumberError{Season: season.Number, Episode: episode.Number}
		}
		seen[episode.Number] = struct{}{}
	}
	return nil
}

func seasonsToV1(seasons map[string]Season) []seasonV1 {
	if len(seasons) == 0 {
		return nil
	}
	keys := make([]int, 0, len(seasons))
	for key, season := range seasons {
		seasonNumber, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		if seasonNumber < 1 {
			continue
		}
		_ = season
		keys = append(keys, seasonNumber)
	}
	sort.Ints(keys)
	out := make([]seasonV1, 0, len(keys))
	for _, seasonNumber := range keys {
		out = append(out, seasonToV1(seasonNumber, seasons[strconv.Itoa(seasonNumber)]))
	}
	return out
}

func seasonsFromV1(seasons []seasonV1) map[string]Season {
	if len(seasons) == 0 {
		return nil
	}
	out := make(map[string]Season, len(seasons))
	for _, season := range seasons {
		out[strconv.Itoa(season.Number)] = seasonFromV1(season)
	}
	return out
}

func seasonToV1Ptr(number int, season *Season) *seasonV1 {
	if season == nil {
		return nil
	}
	disk := seasonToV1(number, *season)
	return &disk
}

func seasonToV1(number int, season Season) seasonV1 {
	return seasonV1{
		Number:   number,
		Notes:    season.Notes,
		Episodes: episodesToV1(season.Episodes),
	}
}

func seasonFromV1Ptr(season *seasonV1) *Season {
	if season == nil {
		return nil
	}
	domain := seasonFromV1(*season)
	return &domain
}

func seasonFromV1(season seasonV1) Season {
	return Season{
		Notes:    season.Notes,
		Episodes: episodesFromV1(season.Episodes),
	}
}

func episodesToV1(episodes map[string]Episode) []episodeV1 {
	if len(episodes) == 0 {
		return nil
	}
	keys := make([]int, 0, len(episodes))
	for key := range episodes {
		episodeNumber, err := strconv.Atoi(key)
		if err != nil || episodeNumber < 1 {
			continue
		}
		keys = append(keys, episodeNumber)
	}
	sort.Ints(keys)
	out := make([]episodeV1, 0, len(keys))
	for _, episodeNumber := range keys {
		episode := episodes[strconv.Itoa(episodeNumber)]
		out = append(out, episodeV1{
			Number:     episodeNumber,
			Media:      mediaFileToV1(episode.Media),
			Companions: companionsToV1(episode.Companions),
		})
	}
	return out
}

func episodesFromV1(episodes []episodeV1) map[string]Episode {
	if len(episodes) == 0 {
		return nil
	}
	out := make(map[string]Episode, len(episodes))
	for _, episode := range episodes {
		key := strconv.Itoa(episode.Number)
		out[key] = Episode{
			Media:      mediaFileFromV1(episode.Media),
			Companions: companionsFromV1(episode.Companions),
		}
	}
	return out
}

func mediaFileToV1(media MediaFile) mediaFileV1 {
	return mediaFileV1{
		Path:       media.Path,
		Source:     cleanSource(media.Source),
		Size:       media.Size,
		MTime:      media.MTime,
		SampleHash: media.SampleHash,
		MediaInfo:  mediaInfoToV1(media.MediaInfo),
	}
}

func mediaFileFromV1(media mediaFileV1) MediaFile {
	return MediaFile{
		Path:       media.Path,
		Source:     cleanSource(media.Source),
		Size:       media.Size,
		MTime:      media.MTime,
		SampleHash: media.SampleHash,
		MediaInfo:  mediaInfoFromV1(media.MediaInfo),
	}
}

func companionsToV1(companions []CompanionFile) []companionFileV1 {
	if len(companions) == 0 {
		return []companionFileV1{}
	}
	out := make([]companionFileV1, 0, len(companions))
	for _, companion := range companions {
		out = append(out, companionFileV1{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime,
		})
	}
	return out
}

func companionsFromV1(companions []companionFileV1) []CompanionFile {
	if len(companions) == 0 {
		return []CompanionFile{}
	}
	out := make([]CompanionFile, 0, len(companions))
	for _, companion := range companions {
		out = append(out, CompanionFile{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime,
		})
	}
	return out
}

func mediaInfoToV1(mediaInfo *MediaInfo) *mediaInfoV1 {
	if mediaInfo == nil {
		return nil
	}
	return &mediaInfoV1{
		VideoCodec:   mediaInfo.VideoCodec,
		Resolution:   mediaInfo.Resolution,
		AudioCodec:   mediaInfo.AudioCodec,
		HasSubtitles: mediaInfo.HasSubtitles,
	}
}

func mediaInfoFromV1(mediaInfo *mediaInfoV1) *MediaInfo {
	if mediaInfo == nil {
		return nil
	}
	return &MediaInfo{
		VideoCodec:   mediaInfo.VideoCodec,
		Resolution:   mediaInfo.Resolution,
		AudioCodec:   mediaInfo.AudioCodec,
		HasSubtitles: mediaInfo.HasSubtitles,
	}
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return append([]string(nil), in...)
}

var (
	seriesV1SchemaOnce sync.Once
	seriesV1Schema     *jsonschema.Schema
	seriesV1SchemaErr  error
)

func validateSeriesV1Schema(series seriesV1) error {
	data, err := json.Marshal(series)
	if err != nil {
		return err
	}
	return validateSeriesV1JSON(data)
}

func validateSeriesV1JSON(data []byte) error {
	schema, err := compiledSeriesV1Schema()
	if err != nil {
		return err
	}
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return err
	}
	return schema.Validate(doc)
}

func compiledSeriesV1Schema() (*jsonschema.Schema, error) {
	seriesV1SchemaOnce.Do(func() {
		var doc any
		decoder := json.NewDecoder(bytes.NewReader(seriesV1SchemaJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			seriesV1SchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("series_v1.schema.json", doc); err != nil {
			seriesV1SchemaErr = err
			return
		}
		seriesV1Schema, seriesV1SchemaErr = compiler.Compile("series_v1.schema.json")
	})
	return seriesV1Schema, seriesV1SchemaErr
}
