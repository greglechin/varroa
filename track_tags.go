package varroa

import (
	"fmt"
	"reflect"

	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/ui"
)

const (
	discNumberLabel   = "DISCNUMBER"
	discTotalLabel    = "TRACKTOTAL"
	releaseTitleLabel = "ALBUM"
	yearLabel         = "DATE" // TODO check if only contains year
	trackArtistLabel  = "ARTIST"
	albumArtistLabel  = "ALBUMARTIST"
	genreLabel        = "GENRE"
	trackTitleLabel   = "TITLE"
	trackNumberLabel  = "TRACKNUMBER"
	trackCommentLabel = "DESCRIPTION"
	composerLabel     = "COMPOSER"
	performerLabel    = "PERFORMER"
	recordLabelLabel  = "ORGANIZATION"
)

var (
	tagFields = []string{"Number",
		"TotalTracks",
		"DiscNumber",
		"Artist",
		"AlbumArtist",
		"Title",
		"Description",
		"Year",
		"Genre",
		"Performer",
		"Composer",
		"Album",
		"Label"}
	tagDescriptions = map[string]string{
		"Number":      "Track Number: ",
		"TotalTracks": "Total Tracks: ",
		"DiscNumber":  "Disc Number: ",
		"Artist":      "Track Artist: ",
		"AlbumArtist": "Album Artist: ",
		"Title":       "Title: ",
		"Description": "Description: ",
		"Year":        "Year: ",
		"Genre":       "Genre: ",
		"Performer":   "Performer: ",
		"Composer":    "Composer: ",
		"Album":       "Album: ",
		"Label":       "Label: ",
	}
)

type TrackTags struct {
	Number      string
	TotalTracks string
	DiscNumber  string
	Artist      string
	AlbumArtist string
	Title       string
	Description string
	Year        string
	Genre       string
	Performer   string
	Composer    string
	Album       string
	Label       string
	OtherTags   map[string]string
}

func NewTrackMetadata(tags map[string]string) (*TrackTags, error) {
	// parse all tags
	tm := &TrackTags{}
	tm.OtherTags = make(map[string]string)
	for k, v := range tags {
		switch k {
		case trackNumberLabel:
			tm.Number = v
		case discNumberLabel:
			tm.DiscNumber = v
		case discTotalLabel:
			tm.TotalTracks = v
		case releaseTitleLabel:
			tm.Album = v
		case yearLabel:
			tm.Year = v
		case trackArtistLabel:
			tm.Artist = v
		case albumArtistLabel:
			tm.AlbumArtist = v
		case genreLabel:
			tm.Genre = v
		case trackTitleLabel:
			tm.Title = v
		case trackCommentLabel:
			tm.Description = v
		case composerLabel:
			tm.Composer = v
		case performerLabel:
			tm.Performer = v
		case recordLabelLabel:
			tm.Label = v
		default:
			// other less common tags
			tm.OtherTags[k] = v
		}
	}
	// TODO detect if we have everything (or at least the required tags)
	// TODO else: trumpable! => return err
	return tm, nil
}

func (tm *TrackTags) String() string {
	normalTags := fmt.Sprintf("Disc#: %s| Track#: %s| Artist: %s| Title: %s| AlbumArtist: %s| Album: %s | TotalTracks: %s| Year: %s| Genre: %s| Performer: %s| Composer: %s| Description: %s| Label: %s", tm.DiscNumber, tm.Number, tm.Artist, tm.Title, tm.AlbumArtist, tm.Album, tm.TotalTracks, tm.Year, tm.Genre, tm.Performer, tm.Composer, tm.Description, tm.Label)
	var extraTags string
	for k, v := range tm.OtherTags {
		extraTags += fmt.Sprintf("%s: %s| ", k, v)
	}
	return normalTags + "| Extra tags: " + extraTags
}

func diffString(title, a, b string) bool {
	if a == b {
		logthis.Info(title+a, logthis.NORMAL)
		return true
	}
	logthis.Info(title+ui.Green(a)+" / "+ui.Red(b), logthis.NORMAL)
	return false
}

func diffField(field string, a, b *TrackTags) bool {
	aField := reflect.ValueOf(a).Elem().FieldByName(field).String()
	bField := reflect.ValueOf(b).Elem().FieldByName(field).String()
	return diffString(tagDescriptions[field], aField, bField)
}

func (tm *TrackTags) diff(o TrackTags) bool {
	isSame := true
	logthis.Info("Comparing A & B:", logthis.NORMAL)
	for _, f := range tagFields {
		isSame = isSame && diffField(f, tm, &o)
	}

	// TODO otherTags

	return isSame
}

func (tm *TrackTags) merge(o TrackTags) error {
	logthis.Info("Merging Track metadata:", logthis.NORMAL)
	for _, f := range tagFields {
		err := tm.mergeFieldByName(f, tagDescriptions[f], o)
		if err != nil {
			return errors.Wrap(err, "error merging "+f)
		}
	}
	// TODO otherTags
	return nil
}

func (tm *TrackTags) mergeFieldByName(field, title string, o TrackTags) error {
	localValue := reflect.ValueOf(tm).Elem().FieldByName(field).String()
	otherValue := reflect.ValueOf(&o).Elem().FieldByName(field).String()
	options := []string{localValue, otherValue}
	if !diffString(title, localValue, otherValue) {
		newValue, err := ui.SelectValue("Select correct value or enter one\n", "First option comes from the audio file, second option from Discogs.", options)
		if err != nil {
			return err
		}
		reflect.ValueOf(tm).Elem().FieldByName(field).SetString(newValue)
		return nil
	}
	return nil
}
