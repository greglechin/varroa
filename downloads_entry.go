package varroa

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/strslice"
	"gitlab.com/catastrophic/assistance/ui"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	stateUnsorted = iota // has metadata but is unsorted
	stateUnused
	stateAccepted // has metadata and has been accepted and exported to library
	stateRejected // has metadata and is not to be exported to library

	currentDownloadsDBSchemaVersion = 1
)

var DownloadFolderStates = []string{"unsorted", "UNUSED", "accepted", "rejected"}

func ColorizeDownloadState(value int, txt string) string {
	switch value {
	case stateAccepted:
		txt = ui.GreenBold(txt)
	case stateUnsorted:
		txt = ui.Blue(txt)
	case stateRejected:
		txt = ui.RedBold(txt)
	}
	return txt
}

func DownloadState(txt string) int {
	switch txt {
	case "accepted":
		return stateAccepted
	case "unsorted":
		return stateUnsorted
	case "rejected":
		return stateRejected
	}
	return -1
}

func IsValidDownloadState(txt string) bool {
	return DownloadState(txt) != -1
}

// -----------------------

type DownloadEntry struct {
	ID                 int      `storm:"id,increment"`
	FolderName         string   `storm:"unique"`
	State              int      `storm:"index"`
	Tracker            []string `storm:"index"`
	TrackerID          []int    `storm:"index"`
	Artists            []string `storm:"index"`
	HasTrackerMetadata bool     `storm:"index"`
	SchemaVersion      int
}

func (d *DownloadEntry) ShortState() string {
	return DownloadFolderStates[d.State][:1]
}

func (d *DownloadEntry) RawShortString() string {
	return fmt.Sprintf("[#%d]\t[%s]\t%s", d.ID, DownloadFolderStates[d.State][:1], d.FolderName)
}

func (d *DownloadEntry) ShortString() string {
	return ColorizeDownloadState(d.State, d.RawShortString())
}

func (d *DownloadEntry) String() string {
	return ColorizeDownloadState(d.State, fmt.Sprintf("ID #%d: %s [%s]", d.ID, d.FolderName, DownloadFolderStates[d.State]))
}

func (d *DownloadEntry) Description(root string) string {
	txt := d.String()
	if d.HasTrackerMetadata {
		txt += "\n"
		for _, t := range d.Tracker {
			txt += string(d.getDescription(root, t, false))
		}
	} else {
		txt += ", does not have any tracker metadata."
	}
	return ColorizeDownloadState(d.State, txt)
}

func (d *DownloadEntry) Load(root string) error {
	if d.FolderName == "" || !fs.DirExists(filepath.Join(root, d.FolderName)) {
		return errors.New("Wrong or missing path")
	}

	// find origin.json
	originFile := filepath.Join(root, d.FolderName, MetadataDir, OriginJSONFile)
	if fs.FileExists(originFile) {
		origin := TrackerOriginJSON{Path: originFile}
		if err := origin.Load(); err != nil {
			return errors.Wrap(err, "Error reading origin.json")
		}
		// TODO: check last update timestamp, compare with value in db
		// TODO: if was not updated, skip.

		// TODO: remove duplicate if there are actually several origins

		// state: should be set to unsorted by default,
		// if it has already been set, leaving it as it is

		// resetting the other fields
		d.Tracker = []string{}
		d.TrackerID = []int{}
		d.Artists = []string{}
		d.HasTrackerMetadata = false
		// if d.SchemaVersion != currentDownloadsDBSchemaVersion {
		//  migration if useful
		// }
		d.SchemaVersion = currentDownloadsDBSchemaVersion

		// load useful things from JSON
		for tracker, info := range origin.Origins {
			d.Tracker = append(d.Tracker, tracker)
			d.TrackerID = append(d.TrackerID, info.ID)

			// getting release info from json
			infoJSON := filepath.Join(root, d.FolderName, MetadataDir, tracker+"_"+trackerMetadataFile)
			infoJSONOldFormat := filepath.Join(root, d.FolderName, MetadataDir, "Release.json")
			if !fs.FileExists(infoJSON) {
				infoJSON = infoJSONOldFormat
			}
			if fs.FileExists(infoJSON) {
				d.HasTrackerMetadata = true

				md := TrackerMetadata{}
				if err := md.LoadFromJSON(tracker, originFile, infoJSON); err != nil {
					return errors.Wrap(err, "Error loading JSON file "+infoJSON)
				}
				// extract relevant information!
				for _, a := range md.Artists {
					d.Artists = append(d.Artists, a.Name)
				}
			}
		}
	} else {
		return errors.New("Error, no metadata found")
	}
	return nil
}

func (d *DownloadEntry) getDescription(root, tracker string, html bool) []byte {
	md, err := d.getMetadata(root, tracker)
	if err != nil {
		return []byte{}
	}
	if html {
		return []byte(md.HTMLDescription())
	}
	return []byte(md.TextDescription(true))
}

func (d *DownloadEntry) getMetadata(root, tracker string) (TrackerMetadata, error) {
	// getting release info from json
	if !d.HasTrackerMetadata {
		return TrackerMetadata{}, errors.New("Error, does not have tracker metadata")
	}

	infoJSON := filepath.Join(root, d.FolderName, MetadataDir, tracker+"_"+trackerMetadataFile)
	if !fs.FileExists(infoJSON) {
		// if not present, try the old format
		infoJSON = filepath.Join(root, d.FolderName, MetadataDir, "Release.json")
	}
	originJSON := filepath.Join(root, d.FolderName, MetadataDir, OriginJSONFile)

	info := TrackerMetadata{}
	err := info.LoadFromJSON(tracker, originJSON, infoJSON)
	if err != nil {
		logthis.Error(errors.Wrap(err, "Error, could not load release json"), logthis.NORMAL)
	}
	return info, err
}

func (d *DownloadEntry) Sort(e *Environment, root string) error {
	// reading metadata
	if err := d.Load(root); err != nil {
		return err
	}
	ui.Header("Sorting " + d.FolderName)
	// if mpd configured, allow playing the release...
	if e.config.MPD != nil && ui.Accept("Load release into MPD") {
		fmt.Println("Sending to MPD.")
		mpdClient := MPD{}
		if err := mpdClient.Connect(e.config.MPD); err == nil {
			defer mpdClient.DisableAndDisconnect(root, d.FolderName)
			if err := mpdClient.SendAndPlay(root, d.FolderName); err != nil {
				fmt.Println(ui.RedBold("Error sending to MPD: " + err.Error()))
			}
		}
	}
	// try to refresh metadata
	if d.HasTrackerMetadata {
		// reading metadata age to quickly check if it is worth refreshing metadata.
		originJSON := filepath.Join(root, d.FolderName, MetadataDir, OriginJSONFile)
		origin := TrackerOriginJSON{Path: originJSON}
		if err := origin.Load(); err == nil {
			// Note: if there is a problem with the file, it'll be found later.
			fmt.Println(ui.Green(origin.lastUpdatedString()))
		}

		if ui.Accept("Try to refresh metadata from tracker") {
			for i, t := range d.Tracker {
				tracker, err := e.Tracker(t)
				if err != nil {
					logthis.Error(errors.Wrap(err, "Error getting configuration for tracker "+t), logthis.NORMAL)
					continue
				}
				if err := RefreshMetadata(e, tracker, []string{strconv.Itoa(d.TrackerID[i])}); err != nil {
					logthis.Error(errors.Wrap(err, "Error refreshing metadata for tracker "+t), logthis.NORMAL)
					continue
				}
			}
		}
	}

	// display metadata
	fmt.Println(d.Description(root))
	ui.Title("Sorting release")
	ui.Usage("This decision will not have any consequence for the files in your download folder, or their seeding status.")
	validChoice := false
	errs := 0
	for !validChoice {
		ui.UserChoice("[A]ccept, [R]eject, or [D]efer decision")
		choice, scanErr := ui.GetInput(nil)
		if scanErr != nil {
			return scanErr
		}

		switch {
		case strings.ToUpper(choice) == "R":
			fmt.Println(ui.RedBold("This release will be considered REJECTED. It will not be removed, but will be ignored in later sorting."))
			fmt.Println(ui.RedBold("This can be reverted by sorting its specific download ID (" + strconv.Itoa(d.ID) + ")."))
			d.State = stateRejected
			validChoice = true
		case strings.ToUpper(choice) == "D":
			fmt.Println(ui.Green("Decision about this download is POSTPONED."))
			d.State = stateUnsorted
			validChoice = true
		case strings.ToUpper(choice) == "A":
			if err := d.export(root, e.config); err != nil {
				return err
			}
			d.State = stateAccepted
			validChoice = true
		}

		if !validChoice {
			fmt.Println(ui.Red("Invalid choice."))
			errs++
			if errs > 10 {
				return errors.New("Error sorting download, too many incorrect choices")
			}
		}
	}
	return nil
}

func (d *DownloadEntry) export(root string, config *Config) error {
	// getting candidates for new folder name
	var candidates []string
	if d.HasTrackerMetadata {
		for _, t := range d.Tracker {
			info, err := d.getMetadata(root, t)
			if err != nil {
				logthis.Info("Could not find metadata for tracker "+t, logthis.NORMAL)
				continue
			}

			// questions about how to file this release
			var artists []string
			for _, a := range info.Artists {
				// not taking feat. artists
				if a.Role == "Main" || a.Role == "Composer" {
					artists = append(artists, a.Name)
				}
			}
			// if only one artist, select them by default
			mainArtist := artists[0]
			if len(artists) > 1 {
				mainArtistCandidates := []string{strings.Join(artists, ", ")}
				mainArtistCandidates = append(mainArtistCandidates, artists...)
				if len(artists) >= 3 {
					mainArtistCandidates = append(mainArtistCandidates, tracker.VariousArtists)
				}

				mainArtist, err = ui.SelectValue("Defining main artist", "If several artists are listed, this will help organize your files.", mainArtistCandidates)
				if err != nil {
					return err
				}
			}
			// retrieving main artist alias from the configuration
			info.MainArtist = mainArtist
			if err = info.checkAliasAndCategory(filepath.Join(root, d.FolderName, MetadataDir)); err != nil {
				return err
			}
			// main artist alias
			aliasCandidates := []string{info.MainArtistAlias}
			if info.MainArtistAlias != info.MainArtist {
				aliasCandidates = append(aliasCandidates, info.MainArtist)
			}
			mainArtistAlias, err := ui.SelectValue("Defining main artist alias", "Change this value to regroup releases from different artist aliases in the library.", aliasCandidates)
			if err != nil {
				return err
			}
			// retrieving category from the configuration
			info.MainArtistAlias = mainArtistAlias
			if err = info.checkAliasAndCategory(filepath.Join(root, d.FolderName, MetadataDir)); err != nil {
				return err
			}
			// category
			categoryCandidates := info.Tags
			if !strslice.Contains(info.Tags, info.Category) {
				categoryCandidates = append([]string{info.Category}, info.Tags...)
			}
			category, err := ui.SelectValue("Defining user category", "Allows custom library organization.", categoryCandidates)
			if err != nil {
				return err
			}
			// saving value
			info.Category = category
			// write to original user_metadata.json
			if err = info.UpdateUserJSON(filepath.Join(root, info.FolderName, MetadataDir), mainArtist, mainArtistAlias, category); err != nil {
				logthis.Error(errors.Wrap(err, "could not update user metadata with main artist, main artists alias, or category"), logthis.NORMAL)
				return err
			}
			// generating new possible paths
			candidates = append(candidates, info.GeneratePath(config.Library.Template, filepath.Join(root, d.FolderName)))
			candidates = append(candidates, info.GeneratePath(defaultFolderTemplate, filepath.Join(root, d.FolderName)))
		}
	}
	// adding current folder name last
	candidates = append(candidates, d.FolderName)
	// select or input a new name
	newName, err := ui.SelectValue("Generating new folder name from metadata", "Folder must not already exist.", candidates)
	if err != nil {
		return err
	}
	if fs.DirExists(filepath.Join(config.Library.Directory, newName)) {
		return errors.New("destination already exists")
	}
	// export
	ui.Title("Exporting release")
	if ui.Accept("Export as " + newName) {
		fmt.Println("Exporting files to the library...")
		if err = fs.CopyDir(filepath.Join(root, d.FolderName), filepath.Join(config.Library.Directory, newName), config.Library.UseHardLinks); err != nil {
			return errors.Wrap(err, "Error exporting download "+d.FolderName)
		}
		fmt.Println(ui.Green("This release has been exported to your library. The original files have not been removed, but will be ignored in later sorts."))
		// if exported, write playlists
		if config.playlistDirectoryConfigured {
			ui.Title("Updating playlists")
			if ui.Accept("Add release to daily/monthly playlists") {
				if err = addReleaseToCurrentPlaylists(config.Library.PlaylistDirectory, config.Library.Directory, newName); err != nil {
					return err
				}
				fmt.Println(ui.Green("Playlists generated or updated.\n"))
			} else {
				fmt.Println(ui.Red("Playlists were not updated to include this release.\n"))
			}
		}
	} else {
		fmt.Println(ui.Red("The release was not exported. It can be exported later by sorting this ID again. Until then, it will be marked as unsorted again.\n"))
		d.State = stateUnsorted
	}
	return nil
}
