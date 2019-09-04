package varroa

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/intslice"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/strslice"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	ReleaseString = `Release info:
	Artist: %s
	Title: %s
	Year: %d
	Release Type: %s
	Format: %s
	Quality: %s
	HasLog: %t
	Log Score: %d
	Has Cue: %t
	Scene: %t
	Source: %s
	Tags: %s
	Torrent URL: %s
	Torrent ID: %s`
	TorrentPath         = `%s - %s (%d) [%s %s %s %s] - %s.torrent`
	TorrentNotification = `%s - %s (%d) [%s/%s/%s/%s]`

	logScoreNotInAnnounce = -9999
)

type Release struct {
	ID          uint32 `storm:"id,increment"`
	Tracker     string `storm:"index"`
	Timestamp   time.Time
	TorrentID   string `storm:"index"`
	GroupID     string
	Artists     []string
	Title       string
	Year        int
	ReleaseType string
	Format      string
	Quality     string
	HasLog      bool
	LogScore    int
	HasCue      bool
	IsScene     bool
	Source      string
	Tags        []string
	torrentURL  string
	Size        uint64
	Folder      string
	Filter      string
}

func NewRelease(trackerName string, parts []string, alternative bool) (*Release, error) {
	if len(parts) != 19 {
		return nil, errors.New("incomplete announce information")
	}

	var tags []string
	var torrentURL, torrentID string
	pattern := `http[s]?://[[:alnum:]\./:]*torrents\.php\?action=download&id=([\d]*)`
	rg := regexp.MustCompile(pattern)

	if alternative {
		tags = strings.Split(parts[16], ",")
		torrentURL = parts[18]

	} else {
		tags = strings.Split(parts[18], ",")
		torrentURL = parts[17]
	}

	// getting torrentID
	hits := rg.FindAllStringSubmatch(torrentURL, -1)
	if len(hits) != 0 {
		torrentID = hits[0][1]
	}
	// cleaning up tags
	for i, el := range tags {
		tags[i] = strings.TrimSpace(el)
	}

	year, err := strconv.Atoi(parts[3])
	if err != nil {
		year = -1
	}
	hasLog := parts[8] != ""
	logScore, err := strconv.Atoi(parts[10])
	if err != nil {
		logScore = logScoreNotInAnnounce
	}
	hasCue := parts[12] != ""
	isScene := parts[15] != ""

	artist := []string{parts[1]}
	// if the raw Artists announce contains & or "performed by", split and add to slice
	subArtists := regexp.MustCompile("&|performed by").Split(parts[1], -1)
	if len(subArtists) != 1 {
		for i, a := range subArtists {
			subArtists[i] = strings.TrimSpace(a)
		}
		artist = append(artist, subArtists...)
	}

	// checks
	releaseType := parts[4]
	if !strslice.Contains(tracker.KnownReleaseTypes, releaseType) {
		return nil, errors.New("Unknown release type: " + releaseType)
	}
	format := parts[5]
	if !strslice.Contains(tracker.KnownFormats, format) {
		return nil, errors.New("Unknown format: " + format)
	}
	source := parts[13]
	if !strslice.Contains(tracker.KnownSources, source) {
		return nil, errors.New("Unknown source: " + source)
	}
	quality := parts[6]
	if !strslice.Contains(tracker.KnownQualities, quality) {
		return nil, errors.New("Unknown quality: " + quality)
	}

	r := &Release{Tracker: trackerName, Timestamp: time.Now(), Artists: artist, Title: parts[2], Year: year, ReleaseType: releaseType, Format: format, Quality: quality, Source: source, HasLog: hasLog, LogScore: logScore, HasCue: hasCue, IsScene: isScene, torrentURL: torrentURL, Tags: tags, TorrentID: torrentID}
	return r, nil
}

func (r *Release) String() string {
	return fmt.Sprintf(ReleaseString, strings.Join(r.Artists, ","), r.Title, r.Year, r.ReleaseType, r.Format, r.Quality, r.HasLog, r.LogScore, r.HasCue, r.IsScene, r.Source, r.Tags, r.torrentURL, r.TorrentID)
}

func (r *Release) ShortString() string {
	short := fmt.Sprintf(TorrentNotification, r.Artists[0], r.Title, r.Year, r.ReleaseType, r.Format, r.Quality, r.Source)
	if r.Size != 0 {
		return short + fmt.Sprintf(" [%s]", humanize.IBytes(r.Size))
	}
	return short
}

func (r *Release) TorrentFile() string {
	torrentFile := fmt.Sprintf(TorrentPath, r.Artists[0], r.Title, r.Year, r.ReleaseType, r.Format, r.Quality, r.Source, r.TorrentID)
	return fs.SanitizePath(torrentFile)
}

func (r *Release) Satisfies(filter *ConfigFilter) bool {
	// no longer filtering on artists. If a filter has artists defined,
	// varroa will now wait until it gets the TorrentInfo and all of the artists
	// to make a call.
	if len(filter.Year) != 0 && !intslice.Contains(filter.Year, r.Year) {
		logthis.Info(filter.Name+": Wrong year", logthis.VERBOSE)
		return false
	}
	if len(filter.Format) != 0 && !strslice.Contains(filter.Format, r.Format) {
		logthis.Info(filter.Name+": Wrong format", logthis.VERBOSE)
		return false
	}
	if len(filter.Source) != 0 && !strslice.Contains(filter.Source, r.Source) {
		logthis.Info(filter.Name+": Wrong source", logthis.VERBOSE)
		return false
	}
	if len(filter.Quality) != 0 && !strslice.Contains(filter.Quality, r.Quality) {
		logthis.Info(filter.Name+": Wrong quality", logthis.VERBOSE)
		return false
	}
	if r.Source == tracker.SourceCD && r.Format == tracker.FormatFLAC && filter.HasLog && !r.HasLog {
		logthis.Info(filter.Name+": Release has no log", logthis.VERBOSE)
		return false
	}
	// only compare logscores if the announce contained that information
	if r.Source == tracker.SourceCD && r.Format == tracker.FormatFLAC && filter.LogScore != 0 && (!r.HasLog || (r.LogScore != logScoreNotInAnnounce && filter.LogScore > r.LogScore)) {
		logthis.Info(filter.Name+": Incorrect log score", logthis.VERBOSE)
		return false
	}
	if r.Source == tracker.SourceCD && r.Format == tracker.FormatFLAC && filter.HasCue && !r.HasCue {
		logthis.Info(filter.Name+": Release has no cue", logthis.VERBOSE)
		return false
	}
	if !filter.AllowScene && r.IsScene {
		logthis.Info(filter.Name+": Scene release not allowed", logthis.VERBOSE)
		return false
	}
	if len(filter.ExcludedReleaseType) != 0 && strslice.Contains(filter.ExcludedReleaseType, r.ReleaseType) {
		logthis.Info(filter.Name+": Excluded release type", logthis.VERBOSE)
		return false
	}
	if len(filter.ReleaseType) != 0 && !strslice.Contains(filter.ReleaseType, r.ReleaseType) {
		logthis.Info(filter.Name+": Wrong release type", logthis.VERBOSE)
		return false
	}
	// checking tags
	if len(filter.TagsRequired) != 0 && !MatchAllInSlice(filter.TagsRequired, r.Tags) {
		logthis.Info(filter.Name+": Does not have all required tags", logthis.VERBOSE)
		return false
	}
	for _, excluded := range filter.TagsExcluded {
		if MatchInSlice(excluded, r.Tags) {
			logthis.Info(filter.Name+": Has excluded tag", logthis.VERBOSE)
			return false
		}
	}
	if len(filter.TagsIncluded) != 0 {
		// if none of r.tags in conf.includedTags, return false
		atLeastOneIncludedTag := false
		for _, t := range r.Tags {
			if MatchInSlice(t, filter.TagsIncluded) {
				atLeastOneIncludedTag = true
				break
			}
		}
		if !atLeastOneIncludedTag {
			logthis.Info(filter.Name+": Does not have any wanted tag", logthis.VERBOSE)
			return false
		}
	}
	// taking the opportunity to retrieve and save some info
	r.Filter = filter.Name
	return true
}

func (r *Release) HasCompatibleTrackerInfo(filter *ConfigFilter, blacklistedUploaders []string, info *TrackerMetadata) bool {
	// checks
	if len(filter.EditionYear) != 0 && !intslice.Contains(filter.EditionYear, info.EditionYear) {
		logthis.Info(filter.Name+": Wrong edition year", logthis.VERBOSE)
		return false
	}
	if filter.MaxSizeMB != 0 && uint64(filter.MaxSizeMB) < (info.Size/(1024*1024)) {
		logthis.Info(filter.Name+": Release too big.", logthis.VERBOSE)
		return false
	}
	if filter.MinSizeMB > 0 && uint64(filter.MinSizeMB) > (info.Size/(1024*1024)) {
		logthis.Info(filter.Name+": Release too small.", logthis.VERBOSE)
		return false
	}
	if r.Source == tracker.SourceCD && r.Format == tracker.FormatFLAC && r.HasLog && filter.LogScore != 0 && filter.LogScore > info.LogScore {
		logthis.Info(filter.Name+": Incorrect log score", logthis.VERBOSE)
		return false
	}
	if len(filter.RecordLabel) != 0 && !MatchInSlice(info.RecordLabel, filter.RecordLabel) {
		logthis.Info(filter.Name+": No match for record label", logthis.VERBOSE)
		return false
	}
	if len(filter.Artist) != 0 || len(filter.ExcludedArtist) != 0 {
		var foundAtLeastOneArtist bool
		for _, iArtist := range info.Artists {
			if MatchInSlice(iArtist.Name, filter.Artist) {
				foundAtLeastOneArtist = true
			}
			if MatchInSlice(iArtist.Name, filter.ExcludedArtist) {
				logthis.Info(filter.Name+": Found excluded artist "+iArtist.Name, logthis.VERBOSE)
				return false
			}
		}
		if !foundAtLeastOneArtist && len(filter.Artist) != 0 {
			logthis.Info(filter.Name+": No match for artists", logthis.VERBOSE)
			return false
		}
	}
	if strslice.Contains(blacklistedUploaders, info.Uploader) || strslice.Contains(filter.BlacklistedUploader, info.Uploader) {
		logthis.Info(filter.Name+": Uploader "+info.Uploader+" is blacklisted.", logthis.VERBOSE)
		return false
	}
	if len(filter.Uploader) != 0 && !strslice.Contains(filter.Uploader, info.Uploader) {
		logthis.Info(filter.Name+": No match for uploader", logthis.VERBOSE)
		return false
	}
	if len(filter.Edition) != 0 {
		found := false
		if MatchInSlice(info.EditionName, filter.Edition) {
			found = true
		}
		if !found {
			logthis.Info(filter.Name+": Edition name does not match any criteria.", logthis.VERBOSE)
			return false
		}
	}
	if filter.RejectUnknown && info.CatalogNumber == "" && info.RecordLabel == "" {
		logthis.Info(filter.Name+": Release has neither a record label or catalog number, rejected.", logthis.VERBOSE)
		return false
	}
	// taking the opportunity to retrieve and save some info
	r.Size = info.Size
	r.LogScore = info.LogScore
	r.Folder = info.FolderName
	r.GroupID = strconv.Itoa(info.GroupID)
	return true
}
