package varroa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/mewkiz/flac"
	"github.com/mgutz/ansi"
	"github.com/pkg/errors"
	"github.com/russross/blackfriday"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/music"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (

	// TODO: add track durations + total duration
	// TODO: lineage: Discogs: XXX; Qobuz: XXX; etc...
	// TODO: add last.fm/discogs/etc info too?
	mdTemplate = `# %s - %s (%d)

![cover](%s)

**Tags:** %s

## Release

**Type:** %s

**Label:** %s

**Catalog Number:** %s

**Source:** %s
%s
## Audio

**Format:** %s

**Quality:** %s

## Tracklist

%s

## Lineage

%s

## Origin

Automatically generated on %s.

Torrent is %s on %s.

Direct link: %s

`
	remasterTemplate = `
**Remaster Label**: %s

**Remaster Catalog Number:** %s

**Remaster Year:** %d

**Edition name:** %s
`
	txtDescription = `
┌──────────
│ %s
└─┬────────
  │  Release Type: %s
  │  Year: %s
  │  Tags: %s
  │  Record Label: %s
  │  Catalog Number: %s
  │  Edition Name: %s
  │  Tracks: %s	
  ├────────
  │  Source: %s
  │  Format: %s
  │  Quality: %s
  ├────────	
  │  Tracker: %s
  │  ID: %s
  │  GroupID: %s
  │  Release Link: %s
  │  Cover: %s	
  │  Size: %s	
  └────────`
	vaReleasePrexif = "VA|"
)

type TrackerMetadataTorrentGroup struct {
	id       int
	name     string
	fullJSON []byte
}

func (tmg *TrackerMetadataTorrentGroup) Load(info *tracker.GazelleTorrentGroup) error {
	// json for metadata, anonymized
	for i := range info.Response.Torrents {
		info.Response.Torrents[i].UserID = 0
		info.Response.Torrents[i].Username = ""
	}
	metadataJSON, err := json.MarshalIndent(info.Response, "", "    ")
	if err != nil {
		return err
	}
	tmg.id = info.Response.Group.ID
	tmg.name = info.Response.Group.Name
	tmg.fullJSON = metadataJSON
	return nil
}

type TrackerMetadataTrack struct {
	Disc     string
	Number   string
	Title    string
	Duration string
	Size     string
}

func (rit *TrackerMetadataTrack) String() string {
	return fmt.Sprintf("+ %s [%s]", rit.Title, rit.Size)
}

type TrackerMetadataArtist struct {
	ID   int
	Name string
	Role string
	JSON []byte `json:"-"`
}

func (tma *TrackerMetadataArtist) Load(info *tracker.GazelleArtist) error {
	// json for metadata
	metadataJSON, err := json.MarshalIndent(info.Response, "", "    ")
	if err != nil {
		return err
	}
	tma.ID = info.Response.ID
	tma.Name = info.Response.Name
	tma.JSON = metadataJSON
	return nil
}

type TrackerMetadataLineage struct {
	Source            string
	LinkOrDescription string
}

type TrackerMetadata struct {
	// JSONs
	ReleaseJSON []byte `json:"-"`
	OriginJSON  []byte `json:"-"`
	// tracker related metadata
	ID           int
	GroupID      int
	Tracker      string
	TrackerURL   string
	ReleaseURL   string
	TimeSnatched int64
	LastUpdated  int64
	IsAlive      bool
	Size         uint64
	Uploader     string
	FolderName   string
	CoverURL     string

	// release related metadata
	Artists       []TrackerMetadataArtist
	Title         string
	Tags          []string
	ReleaseType   string
	RecordLabel   string
	CatalogNumber string
	OriginalYear  int
	EditionName   string
	EditionYear   int
	Source        string
	SourceFull    string
	Format        string
	Quality       string
	LogScore      int
	HasLog        bool
	HasCue        bool
	IsScene       bool
	// for library organization
	MainArtist      string
	MainArtistAlias string
	Category        string
	// contents
	Tracks      []TrackerMetadataTrack
	TotalTime   string
	Lineage     []TrackerMetadataLineage
	Description string
	// current tracker state
	CurrentSeeders int  `json:"-"`
	Reported       bool `json:"-"`
}

func (tm *TrackerMetadata) LoadFromJSON(tracker string, originJSON, releaseJSON string) error {
	if !fs.FileExists(originJSON) || !fs.FileExists(releaseJSON) {
		return errors.New("error loading file " + releaseJSON + " or " + releaseJSON + ", which could not be found")
	}

	// load Origin JSON
	var err error
	origin := TrackerOriginJSON{Path: originJSON}
	if err = origin.Load(); err != nil {
		return err
	}
	// getting the information
	tm.TimeSnatched = origin.Origins[tracker].TimeSnatched
	tm.LastUpdated = origin.Origins[tracker].LastUpdatedMetadata
	tm.IsAlive = origin.Origins[tracker].IsAlive
	tm.Tracker = tracker
	tm.TrackerURL = origin.Origins[tracker].Tracker

	// load Release JSON
	tm.ReleaseJSON, err = ioutil.ReadFile(releaseJSON)
	if err != nil {
		return errors.Wrap(err, "Error loading JSON file "+releaseJSON)
	}
	return tm.loadReleaseJSONFromBytes(filepath.Dir(releaseJSON), true)
}

func (tm *TrackerMetadata) saveOriginJSON(destination string) error {
	origin := &TrackerOriginJSON{Path: filepath.Join(destination, OriginJSONFile)}

	foundOrigin := false
	if fs.FileExists(origin.Path) {
		if err := origin.Load(); err != nil {
			return err
		}
		for i, o := range origin.Origins {
			if i == tm.Tracker && o.ID == tm.ID {
				origin.Origins[i].LastUpdatedMetadata = tm.LastUpdated
				origin.Origins[i].IsAlive = tm.IsAlive
				// may have been edited
				origin.Origins[i].GroupID = tm.GroupID
				foundOrigin = true
			}
		}
	}
	if !foundOrigin {
		if origin.Origins == nil {
			origin.Origins = make(map[string]*OriginJSON)
		}
		// creating origin
		origin.Origins[tm.Tracker] = &OriginJSON{Tracker: tm.TrackerURL, ID: tm.ID, GroupID: tm.GroupID, TimeSnatched: tm.TimeSnatched, LastUpdatedMetadata: tm.LastUpdated, IsAlive: tm.IsAlive}
	}
	return origin.write()
}

func (tm *TrackerMetadata) Load(t *tracker.Gazelle, info *tracker.GazelleTorrent) error {
	// recreate Origin JSON data from tracker
	tm.Tracker = t.Name
	tm.TrackerURL = t.DomainURL
	tm.TimeSnatched = time.Now().Unix()
	tm.LastUpdated = time.Now().Unix()
	tm.IsAlive = true
	return tm.loadFromGazelle(info)
}

// LoadFromID directly from tracker
func (tm *TrackerMetadata) LoadFromID(t *tracker.Gazelle, id string) error {
	// get data from tracker & load
	torrentID, convErr := strconv.Atoi(id)
	if convErr != nil {
		return errors.Wrap(convErr, errorCouldNotGetTorrentInfo)
	}
	gzTorrent, err := t.GetTorrent(torrentID)
	if err != nil {
		return errors.Wrap(err, errorCouldNotGetTorrentInfo)
	}
	return tm.Load(t, gzTorrent)
}

func (tm *TrackerMetadata) loadReleaseJSONFromBytes(parentFolder string, responseOnly bool) error {
	var gt tracker.GazelleTorrent
	var unmarshalErr error
	if responseOnly {
		unmarshalErr = json.Unmarshal(tm.ReleaseJSON, &gt.Response)
	} else {
		unmarshalErr = json.Unmarshal(tm.ReleaseJSON, &gt)
	}
	if unmarshalErr != nil {
		logthis.Error(errors.Wrap(unmarshalErr, "Error parsing torrent info JSON"), logthis.NORMAL)
		return nil
	}
	if err := tm.loadFromGazelle(&gt); err != nil {
		logthis.Error(errors.Wrap(unmarshalErr, "Error parsing torrent info"), logthis.NORMAL)
		return err
	}

	// finally, load user JSON for overwriting user-defined values, if loading from files
	if responseOnly {
		if err := tm.LoadUserJSON(parentFolder); err != nil {
			return err
		}
	}
	// try to find if the configuration has overriding artist aliases/categories
	return tm.checkAliasAndCategory(parentFolder)
}

func (tm *TrackerMetadata) loadFromGazelle(info *tracker.GazelleTorrent) error {
	// tracker related metadata
	tm.ID = info.Response.Torrent.ID
	tm.ReleaseURL = tm.TrackerURL + fmt.Sprintf("/torrents.php?torrentid=%d", info.Response.Torrent.ID)
	tm.GroupID = info.Response.Group.ID
	tm.Size = uint64(info.Response.Torrent.Size)
	// keeping a copy of uploader before anonymizing
	tm.Uploader = info.Response.Torrent.Username
	tm.FolderName = html.UnescapeString(info.Response.Torrent.FilePath)
	tm.CoverURL = info.Response.Group.WikiImage
	tm.CurrentSeeders = info.Response.Torrent.Seeders
	tm.Reported = info.Response.Torrent.Reported

	// release related metadata
	// for now, using artists, composers, "with" categories
	// also available: .Conductor, .Dj, .Producer, .RemixedBy
	for _, el := range info.Response.Group.MusicInfo.Artists {
		tm.Artists = append(tm.Artists, TrackerMetadataArtist{ID: el.ID, Name: html.UnescapeString(el.Name), Role: "Main"})
	}
	for _, el := range info.Response.Group.MusicInfo.With {
		tm.Artists = append(tm.Artists, TrackerMetadataArtist{ID: el.ID, Name: html.UnescapeString(el.Name), Role: "Featuring"})
	}
	for _, el := range info.Response.Group.MusicInfo.Composers {
		tm.Artists = append(tm.Artists, TrackerMetadataArtist{ID: el.ID, Name: html.UnescapeString(el.Name), Role: "Composer"})
	}
	tm.Title = html.UnescapeString(info.Response.Group.Name)
	tm.Tags = info.Response.Group.Tags
	tm.ReleaseType = tracker.GazelleReleaseType(info.Response.Group.ReleaseType)
	tm.RecordLabel = html.UnescapeString(info.Response.Group.RecordLabel)
	if info.Response.Torrent.Remastered && info.Response.Torrent.RemasterRecordLabel != "" {
		tm.RecordLabel = html.UnescapeString(info.Response.Torrent.RemasterRecordLabel)
	}
	tm.CatalogNumber = info.Response.Group.CatalogueNumber
	if info.Response.Torrent.Remastered && info.Response.Torrent.RemasterCatalogueNumber != "" {
		tm.CatalogNumber = info.Response.Torrent.RemasterCatalogueNumber
	}
	tm.OriginalYear = info.Response.Group.Year
	tm.EditionName = html.UnescapeString(info.Response.Torrent.RemasterTitle)
	tm.EditionYear = info.Response.Torrent.RemasterYear
	tm.Source = html.UnescapeString(info.Response.Torrent.Media)
	tm.Format = info.Response.Torrent.Format
	tm.Quality = info.Response.Torrent.Encoding
	tm.LogScore = info.Response.Torrent.LogScore
	tm.HasLog = info.Response.Torrent.HasLog
	tm.HasCue = info.Response.Torrent.HasCue
	tm.IsScene = info.Response.Torrent.Scene

	tm.SourceFull = tm.Source
	if tm.SourceFull == tracker.SourceCD && tm.Quality == tracker.QualityLossless {
		if tm.HasLog && tm.HasCue && (tm.LogScore == 100 || info.Response.Torrent.Grade == "Silver") {
			tm.SourceFull += "+"
		}
		if info.Response.Torrent.Grade == "Gold" {
			tm.SourceFull += "+"
		}
	}

	// parsing info that needs to be worked on before use

	// default organization info
	var artists []string
	for _, a := range tm.Artists {
		// not taking feat. artists
		if a.Role == "Main" || a.Role == "Composer" {
			artists = append(artists, a.Name)
		}
	}
	tm.MainArtist = strings.Join(artists, ", ")
	if len(artists) >= 3 {
		tm.MainArtist = tracker.VariousArtists
	}

	// default: artist alias = main artist
	tm.MainArtistAlias = tm.MainArtist
	// default: category == first tag
	if len(tm.Tags) != 0 {
		tm.Category = tm.Tags[0]
	} else {
		tm.Category = "UNKNOWN"
	}

	// parsing track list
	r := regexp.MustCompile(tracker.TrackPattern)
	files := strings.Split(info.Response.Torrent.FileList, "|||")
	for _, f := range files {
		track := TrackerMetadataTrack{}
		hits := r.FindAllStringSubmatch(f, -1)
		if len(hits) != 0 {
			// TODO instead of path, actually find the title
			// only detect actual music files
			track.Title = html.UnescapeString(hits[0][1])
			size, _ := strconv.ParseUint(hits[0][2], 10, 64)
			track.Size = humanize.IBytes(size)
			tm.Tracks = append(tm.Tracks, track)
			// TODO Duration  + Disc + number
		}
		if len(tm.Tracks) == 0 {
			logthis.Info("Could not parse filelist, no music tracks found.", logthis.VERBOSEST)
		}
	}
	// TODO tm.TotalTime

	// TODO find other info, parse for discogs/musicbrainz/itunes links in both descriptions
	if info.Response.Torrent.Description != "" {
		tm.Lineage = append(tm.Lineage, TrackerMetadataLineage{Source: "TorrentDescription", LinkOrDescription: html.UnescapeString(info.Response.Torrent.Description)})
	}
	// TODO add info.Response.Torrent.Lineage if not empty?

	// TODO de-wikify
	tm.Description = html.UnescapeString(info.Response.Group.WikiBody)

	// json for metadata, anonymized
	info.Response.Torrent.Username = ""
	info.Response.Torrent.UserID = 0
	// keeping a copy of the full JSON
	metadataJSON, err := json.MarshalIndent(info.Response, "", "    ")
	if err != nil {
		metadataJSON = tm.ReleaseJSON // falling back to complete json
	}
	tm.ReleaseJSON = metadataJSON
	return nil
}

func (tm *TrackerMetadata) checkAliasAndCategory(parentFolder string) error {
	conf, configErr := NewConfig(DefaultConfigurationFile)
	if configErr != nil {
		return configErr
	}
	if conf.LibraryConfigured {
		var changed bool
		// try to find main artist alias
		for alias, aliasArtists := range conf.Library.Aliases {
			if artistInSlice(tm.MainArtist, tm.Title, aliasArtists) {
				tm.MainArtistAlias = alias
				changed = true
				break
			}
		}
		// try to find category for main artist alias
		for category, categoryArtists := range conf.Library.Categories {
			if artistInSlice(tm.MainArtistAlias, tm.Title, categoryArtists) {
				tm.Category = category
				changed = true
				break
			}
		}
		if changed {
			logthis.Info("Updating user metadata with information from the configuration.", logthis.VERBOSEST)
			return tm.UpdateUserJSON(parentFolder, tm.MainArtist, tm.MainArtistAlias, tm.Category)
		}
	}
	return nil
}

// artistInSlice checks if an artist is in a []string (taking VA releases into account), returns bool.
func artistInSlice(artist, title string, list []string) bool {
	for _, b := range list {
		if artist == b || artist == tracker.VariousArtists && title == strings.TrimSpace(strings.Replace(b, vaReleasePrexif, "", -1)) {
			return true
		}
	}
	return false
}

// SaveFromTracker all of the relevant metadata.
func (tm *TrackerMetadata) SaveFromTracker(parentFolder string, t *tracker.Gazelle) error {
	destination := filepath.Join(parentFolder, MetadataDir)
	// create metadata dir if necessary
	if err := os.MkdirAll(destination, 0775); err != nil {
		return errors.Wrap(err, errorCreatingMetadataDir)
	}

	// creating or updating origin.json
	if err := tm.saveOriginJSON(destination); err != nil {
		return errors.Wrap(err, errorWithOriginJSON)
	}

	// NOTE: errors are not returned (for now) in case the next JSONs can be retrieved

	// write tracker metadata to target folder
	if err := ioutil.WriteFile(filepath.Join(destination, tm.Tracker+"_"+trackerMetadataFile), tm.ReleaseJSON, 0666); err != nil {
		logthis.Error(errors.Wrap(err, errorWritingJSONMetadata), logthis.NORMAL)
	} else {
		logthis.Info(infoMetadataSaved+filepath.Base(parentFolder), logthis.VERBOSE)
	}

	// get torrent group info
	gzTorrentGroup, err := t.GetTorrentGroup(tm.GroupID)
	if err != nil {
		logthis.Info(fmt.Sprintf(errorRetrievingTorrentGroupInfo, tm.GroupID), logthis.NORMAL)
	} else {
		torrentGroupInfo := &TrackerMetadataTorrentGroup{}
		if e := torrentGroupInfo.Load(gzTorrentGroup); e != nil {
			logthis.Info(fmt.Sprintf(errorRetrievingTorrentGroupInfo, tm.GroupID), logthis.NORMAL)
		}
		// write tracker artist metadata to target folder
		if e := ioutil.WriteFile(filepath.Join(destination, tm.Tracker+"_"+trackerTGroupMetadataFile), torrentGroupInfo.fullJSON, 0666); e != nil {
			logthis.Error(errors.Wrap(e, errorWritingJSONMetadata), logthis.NORMAL)
		} else {
			logthis.Info(fmt.Sprintf(infoTorrentGroupMetadataSaved, tm.Title, filepath.Base(parentFolder)), logthis.VERBOSE)
		}
	}

	// get artist info
	for _, a := range tm.Artists {
		gzArtist, err := t.GetArtist(a.ID)
		if err != nil {
			logthis.Info(fmt.Sprintf(errorRetrievingArtistInfo, a.ID), logthis.NORMAL)
			continue
		}
		artistInfo := &TrackerMetadataArtist{}
		if e := artistInfo.Load(gzArtist); e != nil {
			logthis.Info(fmt.Sprintf(errorRetrievingArtistInfo, a.ID), logthis.NORMAL)
			continue
		}
		// write tracker artist metadata to target folder
		// making sure the artistInfo.name+jsonExt is a valid filename
		if e := ioutil.WriteFile(filepath.Join(destination, t.Name+"_"+a.Name+jsonExt), artistInfo.JSON, 0666); e != nil {
			logthis.Error(errors.Wrap(e, errorWritingJSONMetadata), logthis.NORMAL)
		} else {
			logthis.Info(fmt.Sprintf(infoArtistMetadataSaved, a.Name, filepath.Base(parentFolder)), logthis.VERBOSE)
		}
	}
	// generate blank user metadata json
	if err := tm.WriteUserJSON(destination); err != nil {
		logthis.Error(errors.Wrap(err, errorGeneratingUserMetadataJSON), logthis.NORMAL)
	}

	// download tracker cover to target folder
	if err := tm.SaveCover(parentFolder); err != nil {
		logthis.Error(errors.Wrap(err, errorDownloadingTrackerCover), logthis.NORMAL)
	} else {
		logthis.Info(infoCoverSaved+filepath.Base(parentFolder), logthis.VERBOSE)
	}
	logthis.Info(fmt.Sprintf(infoAllMetadataSaved, t.Name), logthis.VERBOSE)
	return nil
}

func (tm *TrackerMetadata) SaveCover(releaseFolder string) error {
	if tm.CoverURL == "" {
		return errors.New("unknown image url")
	}
	filename := filepath.Join(releaseFolder, MetadataDir, tm.Tracker+"_"+trackerCoverFile+filepath.Ext(tm.CoverURL))

	if fs.FileExists(filename) {
		// already downloaded, or exists in folder already: do nothing
		return nil
	}
	response, err := http.Get(tm.CoverURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, response.Body)
	return err
}

func (tm *TrackerMetadata) HTMLDescription() string {

	// TODO use HTML template directly!!

	if tm.Title == "" {
		return "No metadata found"
	}
	// artists
	// TODO separate main from guests
	artists := ""
	for i, a := range tm.Artists {
		artists += a.Name
		if i != len(tm.Artists)-1 {
			artists += ", "
		}
	}
	// tracklist
	tracklist := ""
	for _, t := range tm.Tracks {
		tracklist += t.String() + "\n"
	}
	// compile remaster info
	remaster := ""
	if tm.EditionName != "" || tm.EditionYear != 0 {
		remaster = fmt.Sprintf(remasterTemplate, tm.RecordLabel, tm.CatalogNumber, tm.EditionYear, tm.EditionName)
	}
	// lineage
	lineage := ""
	for _, l := range tm.Lineage {
		lineage += fmt.Sprintf("**%s**: %s\n", l.Source, l.LinkOrDescription)
	}
	// alive
	isAlive := "still registered"
	if !tm.IsAlive {
		isAlive = "unregistered"
	}
	// general output
	md := fmt.Sprintf(mdTemplate, artists, tm.Title, tm.OriginalYear, tm.CoverURL, strings.Join(tm.Tags, ", "),
		tm.ReleaseType, tm.RecordLabel, tm.CatalogNumber, tm.Source, remaster, tm.Format, tm.Quality, tracklist,
		lineage, time.Now().Format("2006-01-02 15:04"), isAlive, tm.Tracker, tm.ReleaseURL)
	return string(blackfriday.Run([]byte(md)))
}

func (tm *TrackerMetadata) TextDescription(fancy bool) string {
	artists := make([]string, len(tm.Artists))
	for i, a := range tm.Artists {
		artists[i] = a.Name
	}
	artistNames := strings.Join(artists, ", ")

	titleStyle := ""
	reset := ""
	style := func(x string) string { return x }
	if fancy {
		titleStyle = ansi.ColorCode("green+hub")
		reset = ansi.ColorCode("reset")
		style = ansi.ColorFunc("blue+hb")
	}
	fullTitle := titleStyle + artistNames + " - " + tm.Title + reset

	year := tm.OriginalYear
	if tm.EditionYear != 0 {
		year = tm.EditionYear
	}

	return fmt.Sprintf(txtDescription,
		fullTitle,
		style(tm.ReleaseType),
		style(fmt.Sprintf("%d", year)),
		style(strings.Join(tm.Tags, ", ")),
		style(tm.RecordLabel),
		style(tm.CatalogNumber),
		style(tm.EditionName),
		style(fmt.Sprintf("%d", len(tm.Tracks))),
		style(tm.SourceFull),
		style(tm.Format),
		style(tm.Quality),
		style(tm.Tracker),
		style(fmt.Sprintf("%d", tm.ID)),
		style(fmt.Sprintf("%d", tm.GroupID)),
		style(tm.ReleaseURL),
		style(tm.CoverURL),
		style(humanize.IBytes(tm.Size)),
	)
}

func getAudioInfo(f string) (string, string, error) {
	stream, err := flac.ParseFile(f)
	if err != nil {
		return "", "", errors.Wrap(err, "could not get FLAC information")
	}
	defer stream.Close()

	format := "FLAC"
	if stream.Info.BitsPerSample == 24 {
		format += "24"
	}

	var sampleRate string
	if stream.Info.SampleRate%1000 == 0 {
		sampleRate = fmt.Sprintf("%d", int32(stream.Info.SampleRate/1000))
	} else {
		sampleRate = fmt.Sprintf("%.1f", float32(stream.Info.SampleRate)/1000)
	}
	return format, sampleRate, nil
}

func getFullAudioFormat(f string) (string, error) {
	format, sampleRate, err := getAudioInfo(f)
	if err != nil {
		return "", err
	}
	if format == "FLAC" && sampleRate == "44.1" {
		return format, nil
	}
	return fmt.Sprintf("%s-%skHz", format, sampleRate), nil
}

func (tm *TrackerMetadata) GeneratePath(folderTemplate, releaseFolder string) string {
	if folderTemplate == "" {
		return tm.FolderName
	}

	// usual edition specifiers, shortened
	editionName := tracker.ShortEdition(tm.EditionName)

	// identifying info
	var idElements []string
	if tm.EditionYear != 0 && tm.EditionYear != tm.OriginalYear {
		idElements = append(idElements, fmt.Sprintf("%d", tm.EditionYear))
	}
	if editionName != "" {
		idElements = append(idElements, editionName)
	}
	// adding catalog number, or if not specified, the record label
	if tm.CatalogNumber != "" {
		idElements = append(idElements, tm.CatalogNumber)
	} else if tm.RecordLabel != "" {
		idElements = append(idElements, tm.RecordLabel)
	} // TODO when we have neither catnum nor label

	var releaseTypeExceptAlbum string
	if tm.ReleaseType != tracker.ReleaseAlbum {
		// adding release type if not album
		releaseTypeExceptAlbum = tm.ReleaseType
	}
	id := strings.Join(idElements, ", ")
	if id == "" {
		id = "Unknown"
	}

	quality := tracker.ShortEncoding(tm.Quality)
	if quality == "FLAC" || quality == "FLAC24" {
		// get one music file then find sample rate
		//firstTrackFilename := filepath.Join(releaseFolder, tm.Tracks[0].Title)
		firstTrackFilename := music.GetFirstFLACFound(releaseFolder)
		fullFormat, err := getFullAudioFormat(firstTrackFilename)
		if err != nil {
			logthis.Error(err, logthis.VERBOSEST)
		} else {
			quality = fullFormat
		}
	}

	r := strings.NewReplacer(
		"$id", "{{$id}}",
		"$a", "{{$a}}",
		"$ma", "{{$ma}}",
		"$c", "{{$c}}",
		"$t", "{{$t}}",
		"$y", "{{$y}}",
		"$f", "{{$f}}",
		"$s", "{{$s}}",
		"$l", "{{$l}}",
		"$n", "{{$n}}",
		"$e", "{{$e}}",
		"$g", "{{$g}}",
		"$r", "{{$r}}",
		"$xar", "{{$xar}}",
		"{", "ÆÆ", // otherwise golang's template throws a fit if '{' or '}' are in the user pattern
		"}", "¢¢", // assuming these character sequences will probably not cause conflicts.
	)

	// replace with all valid epub parameters
	tmpl := fmt.Sprintf(`{{$c := %q}}{{$ma := %q}}{{$a := %q}}{{$y := "%d"}}{{$t := %q}}{{$f := %q}}{{$s := %q}}{{$g := %q}}{{$l := %q}}{{$n := %q}}{{$e := %q}}{{$id := %q}}{{$r := %q}}{{$xar := %q}}%s`,
		fs.SanitizePath(tm.Category),
		fs.SanitizePath(tm.MainArtistAlias),
		fs.SanitizePath(tm.MainArtist),
		tm.OriginalYear,
		fs.SanitizePath(tm.Title),
		quality,
		tm.Source,
		tm.SourceFull, // source with indicator if 100%/log/cue or Silver/gold
		fs.SanitizePath(tm.RecordLabel),
		tm.CatalogNumber,
		fs.SanitizePath(editionName), // edition
		fs.SanitizePath(id),          // identifying info
		tm.ReleaseType,
		releaseTypeExceptAlbum,
		r.Replace(folderTemplate))

	var doc bytes.Buffer
	te := template.Must(template.New("hop").Parse(tmpl))
	if err := te.Execute(&doc, nil); err != nil {
		return tm.FolderName
	}
	newName := strings.TrimSpace(doc.String())
	// trim spaces around all internal folder names
	var trimmedParts = strings.Split(newName, "/")
	for i, part := range trimmedParts {
		trimmedParts[i] = strings.TrimSpace(part)
	}
	// recover brackets
	r2 := strings.NewReplacer(
		"ÆÆ", "{",
		"¢¢", "}",
	)
	return r2.Replace(strings.Join(trimmedParts, "/"))
}

func (tm *TrackerMetadata) WriteUserJSON(destination string) error {
	userJSON := filepath.Join(destination, userMetadataJSONFile)
	if fs.FileExists(userJSON) {
		logthis.Info("user metadata JSON already exists", logthis.VERBOSE)
		return nil
	}
	// save as blank JSON, with no values, for the user to force metadata values if needed.
	blank := TrackerMetadata{}
	blank.Artists = append(blank.Artists, TrackerMetadataArtist{})
	blank.Tracks = append(blank.Tracks, TrackerMetadataTrack{})
	blank.Lineage = append(blank.Lineage, TrackerMetadataLineage{})
	blank.HasLog = tm.HasLog
	blank.HasCue = tm.HasCue
	blank.IsScene = tm.IsScene
	blank.MainArtist = tm.MainArtist
	blank.MainArtistAlias = tm.MainArtistAlias
	blank.Category = tm.Category
	metadataJSON, err := json.MarshalIndent(blank, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(userJSON, metadataJSON, 0644)
}

func (tm *TrackerMetadata) UpdateUserJSON(destination, mainArtist, mainArtistAlias, category string) error {
	userJSON := filepath.Join(destination, userMetadataJSONFile)
	if !fs.FileExists(userJSON) {
		// try to create the file
		if err := tm.WriteUserJSON(destination); err != nil {
			return errors.New("user metadata JSON does not already exist and could not be written")
		}
	}

	// loading user metadata file
	userJSONBytes, err := ioutil.ReadFile(userJSON)
	if err != nil {
		return errors.New("could not read user JSON")
	}
	var userInfo *TrackerMetadata
	if unmarshalErr := json.Unmarshal(userJSONBytes, &userInfo); unmarshalErr != nil {
		logthis.Info("error parsing torrent info JSON", logthis.NORMAL)
		return nil
	}
	// overwriting select values
	// NOTE: since we are sorting from the downloads folder to the library, there is no reason why these values would have been set by the user
	// So nothing should be lost.
	userInfo.MainArtist = mainArtist
	userInfo.MainArtistAlias = mainArtistAlias
	userInfo.Category = category
	// write back
	metadataJSON, err := json.MarshalIndent(userInfo, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(userJSON, metadataJSON, 0644)
}

func (tm *TrackerMetadata) LoadUserJSON(parentFolder string) error {
	userJSON := filepath.Join(parentFolder, userMetadataJSONFile)
	if !fs.FileExists(userJSON) {
		logthis.Info("user metadata JSON does not exist", logthis.VERBOSEST)
		return nil
	}
	// loading user metadata file
	userJSONBytes, err := ioutil.ReadFile(userJSON)
	if err != nil {
		return errors.New("could not read user JSON")
	}
	var userInfo *TrackerMetadata
	if unmarshalErr := json.Unmarshal(userJSONBytes, &userInfo); unmarshalErr != nil {
		logthis.Info("error parsing torrent info JSON", logthis.NORMAL)
		return nil
	}
	//  overwrite tracker values if non-zero value found
	s := reflect.ValueOf(tm).Elem()
	s2 := reflect.ValueOf(userInfo).Elem()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		f2 := s2.Field(i)
		if f.Type().String() == "string" && f2.String() != "" {
			f.Set(f2)
		}
		if (f.Type().String() == "int" || f.Type().String() == "int64") && f2.Int() != 0 {
			f.Set(f2)
		}
		// NOTE: nothing is done with boolean values. Hard to say if the value read is the default one or user-defined.
	}
	// more complicated types
	if len(userInfo.Tags) != 0 {
		// TODO or concatenate lists?
		tm.Tags = userInfo.Tags
	}
	// if artists are defined which are not blank
	if len(userInfo.Artists) != 0 {
		if userInfo.Artists[0].Name != "" {
			tm.Artists = userInfo.Artists
		}
	}
	if len(userInfo.Tracks) != 0 {
		if userInfo.Tracks[0].Title != "" {
			tm.Tracks = userInfo.Tracks
		}
	}
	if len(userInfo.Lineage) != 0 {
		if userInfo.Lineage[0].Source != "" {
			tm.Lineage = userInfo.Lineage
		}
	}
	return nil
}

func (tm *TrackerMetadata) Release() *Release {
	r := &Release{Tracker: tm.Tracker, Timestamp: time.Now()}
	// for now, using artists, composers, "with" categories
	for _, a := range tm.Artists {
		r.Artists = append(r.Artists, a.Name)
	}
	r.Title = tm.Title
	if tm.EditionYear != 0 {
		r.Year = tm.EditionYear
	} else {
		r.Year = tm.OriginalYear
	}
	r.ReleaseType = tm.ReleaseType
	r.Format = tm.Format
	r.Quality = tm.Quality
	r.HasLog = tm.HasLog
	r.HasCue = tm.HasCue
	r.IsScene = tm.IsScene
	r.Source = tm.Source
	r.Tags = tm.Tags
	// r.url =
	// r.torrentURL =
	r.TorrentID = fmt.Sprintf("%d", tm.ID)
	r.GroupID = fmt.Sprintf("%d", tm.GroupID)
	// r.TorrentFile =
	r.Size = tm.Size
	r.Folder = tm.FolderName
	r.LogScore = tm.LogScore
	return r
}

// IsWellSeeded if it has more than minimumSeeders.
func (tm *TrackerMetadata) IsWellSeeded() bool {
	return tm.CurrentSeeders >= minimumSeeders
}
