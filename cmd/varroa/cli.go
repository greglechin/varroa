package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	docopt "github.com/docopt/docopt-go"
	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/intslice"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/strslice"
	"gitlab.com/passelecasque/varroa"
)

const (
	varroaUsage = `
	_  _ ____ ____ ____ ____ ____    _  _ _  _ ____ _ ____ ____
	|  | |__| |__/ |__/ |  | |__|    |\/| |  | [__  | |    |__|
	 \/  |  | |  \ |  \ |__| |  |    |  | |__| ___] | |___ |  |


Description:

	varroa musica is a personal assistant for your favorite tracker.

	It can:
	- snatch, and autosnatch torrents with quite thorough filters
	- monitor your stats and generate graphs
	- host said graphs on its embedded webserver or on Gitlab Pages
	- save and update all snatched torrents metadata
	- be remotely controlled from your browser with a GreaseMonkey script.
	- send notifications to your Android device or to a given IRC user 
	  about stats and snatches.
	- check local logs against logchecker.php
	- sort downloads, export them to your library, automatically rename
	  folders using tracker metadata
	- mount a read-only FUSE filesystem exposing your downloads or library
	  using tracker metadata

Daemon Commands:

	The daemon is used for autosnatching, stats monitoring and hosting,
	and remotely triggering snatches from the GM script or any
	pyWhatAuto remote (including the Android App).

	start:
		starts the daemon.
	stop:
		stops it.
	uptime:
		shows how long it has been running.
	status
		returns information about the daemon status.

Commands:

	stats:
		generates the stats immediately based on currently saved
		history.
	refresh-metadata:
		retrieves all metadata for releases with the given local
		path, updating the files that were downloaded when they
		were first snatched (allows updating local metadata if a
		torrent has been edited since upload).
	refresh-metadata-by-id:
		retrieves all metadata for all torrents with IDs given as
		arguments, updating the files that were downloaded when they
		were first snatched (allows updating local metadata if a
		torrent has been edited since upload).
	check-log:
		upload a given log file to the tracker's logchecker.php and
		returns its score.
	snatch:
		snatch all torrents with IDs given as arguments.
	info:
		output info about the torrent IDs given as argument.
	backup:
		backup user files (stats, history, configuration file) to a
		timestamped zip file. Automatically triggered every day.
	downloads search:
		return all known downloads on which an artist has worked.
	downloads metadata:
		return information about a specific download. Takes downloads
		db ID as argument.
	downloads sort:
		sort all unsorted downloads, or sort a specific release
		(identified by its path). sorting allows you to tag which
		release to keep and which to only seed; selected downloads
		can be exported to an external folder.
	downloads sort-id:
		sort all unsorted downloads, or sort a specific release
		(identified by its db ID). sorting allows you to tag which
		release to keep and which to only seed; selected downloads
		can be exported to an external folder.
	downloads list:
		list all downloads, of filter by state: unsorted, accepted, 
	    exported, rejected.
	downloads clean:
		clean up the downloads directory by moving all empty folders,
		and folders with only tracker metadata, to a dedicated subfolder.
	downloads fuse:
		mount a read-only filesystem exposing your downloads using the
		tracker metadata, using the following categories: artists, tags,
		record labels, years. Call 'fusermount -u MOUNT_POINT' to stop.
	library reorganize:
		renames all releases in the library (including parent folders) 
		using tracker metadata and the user-defined folder template.
	library fuse:
		similar to downloads fuse, but for your music library.
	reseed:
		reseed a downloaded release using tracker metadata. Does not check
		the torrent files actually match the contents in the given PATH.
	
Configuration Commands:

	show-config:
		displays what varroa has parsed from the configuration file
		(useful for checking the YAML is correctly formatted, and the
		filters are correctly interpreted).
	encrypt:
		encrypts your configuration file. The encrypted version can
		be used in place of the plaintext version, if you're
		uncomfortable having passwords lying around in an simple text
		file. You will be prompted for a passphrase which you will
		have to enter again every time you run varroa. Your passwords
		will still be decoded in memory while varroa is up. This
		command does not remove the plaintext version.
	decrypt:
		decrypts your encrypted configuration file.

Usage:
	varroa (start [--no-daemon]|stop|uptime|status)
	varroa stats
	varroa refresh-metadata <PATH>...
	varroa refresh-metadata-by-id <TRACKER> <ID>...
	varroa check-log <TRACKER> <LOG_FILE>
	varroa snatch [--fl] <TRACKER> <ID>...
	varroa info <TRACKER> <ID>...
	varroa backup
	varroa show-config
	varroa (downloads|dl) (search <ARTIST>|metadata <ID>|sort [--new] [<PATH>...]|sort-id [<ID>...]|list [<STATE>]|clean|fuse <MOUNT_POINT>)
	varroa library (fuse <MOUNT_POINT>|reorganize [--simulate|--interactive])
	varroa reseed <TRACKER> <PATH>
	varroa (encrypt|decrypt)
	varroa --version

Options:
 	-h, --help             Show this screen.
	--no-daemon            Starts varroa but without turning it into a daemon. No log will be kept. Ctrl+C to quit.
 	--fl                   Use personal Freeleech torrent if available.
	--simulate             Simulate library reorganization to show what would be renamed.
	--interactive          Library reorganization requires user confirmation for each release if necessary.
	--new                  Only sort new releases (ignore previously sorted ones)
  	--version              Show version.
`
)

type refreshTarget struct {
	path    string
	tracker string
	id      int
}

type varroaArguments struct {
	builtin                 bool
	start                   bool
	noDaemon                bool
	stop                    bool
	uptime                  bool
	status                  bool
	stats                   bool
	refreshMetadata         bool
	refreshMetadataByID     bool
	checkLog                bool
	snatch                  bool
	info                    bool
	backup                  bool
	showConfig              bool
	encrypt                 bool
	decrypt                 bool
	downloadSearch          bool
	downloadInfo            bool
	downloadSort            bool
	downloadSortID          bool
	downloadList            bool
	downloadState           string
	downloadClean           bool
	downloadFuse            bool
	libraryFuse             bool
	libraryReorg            bool
	libraryReorgInteractive bool
	libraryReorgSimulate    bool
	reseed                  bool
	enhance                 bool
	useFLToken              bool
	ignoreSorted            bool
	torrentIDs              []int
	logFile                 string
	trackerLabel            string
	paths                   []string
	artistName              string
	mountPoint              string
	requiresDaemon          bool
	canUseDaemon            bool
	toRefresh               []refreshTarget
}

func (b *varroaArguments) parseCLI(osArgs []string) error {
	// parse arguments and options
	args, err := docopt.Parse(varroaUsage, osArgs, true, fmt.Sprintf(varroa.FullVersion, varroa.FullName, varroa.Version), false, false)
	if err != nil {
		return errors.Wrap(err, varroa.ErrorInfoBadArguments)
	}
	if len(args) == 0 {
		// builtin command, nothing to do.
		b.builtin = true
		return nil
	}
	// commands
	b.start = args["start"].(bool)
	b.noDaemon = args["--no-daemon"].(bool)
	b.stop = args["stop"].(bool)
	b.uptime = args["uptime"].(bool)
	b.status = args["status"].(bool)
	b.stats = args["stats"].(bool)
	b.reseed = args["reseed"].(bool)
	//b.enhance = args["enhance"].(bool)
	b.refreshMetadataByID = args["refresh-metadata-by-id"].(bool)
	b.refreshMetadata = args["refresh-metadata"].(bool)
	b.checkLog = args["check-log"].(bool)
	b.snatch = args["snatch"].(bool)
	b.backup = args["backup"].(bool)
	b.info = args["info"].(bool)
	b.showConfig = args["show-config"].(bool)
	b.encrypt = args["encrypt"].(bool)
	b.decrypt = args["decrypt"].(bool)
	if args["downloads"].(bool) || args["dl"].(bool) {
		b.downloadSearch = args["search"].(bool)
		if b.downloadSearch {
			b.artistName = args["<ARTIST>"].(string)
		}
		b.downloadInfo = args["metadata"].(bool)
		b.downloadSort = args["sort"].(bool)
		if b.downloadSort {
			b.ignoreSorted = args["--new"].(bool)
		}
		b.downloadSortID = args["sort-id"].(bool)
		b.downloadList = args["list"].(bool)
		b.downloadClean = args["clean"].(bool)
		b.downloadFuse = args["fuse"].(bool)
	}
	if args["library"].(bool) {
		b.libraryFuse = args["fuse"].(bool)
		b.libraryReorg = args["reorganize"].(bool)
		b.libraryReorgSimulate = args["--simulate"].(bool)
		b.libraryReorgInteractive = args["--interactive"].(bool)
	}
	if b.reseed || b.downloadSort || b.enhance {
		b.paths = args["<PATH>"].([]string)
		for i, p := range b.paths {
			if !fs.DirExists(p) {
				return errors.New("target path does not exist")
			}
			if !varroa.DirectoryContainsMusicAndMetadata(p) {
				return fmt.Errorf(varroa.ErrorFindingMusicAndMetadata, p)
			}
			if strings.HasSuffix(p, "/") {
				b.paths[i] = p[:len(p)-1]
			}
		}
	}
	// arguments
	if b.refreshMetadataByID || b.snatch || b.downloadInfo || b.downloadSortID || b.info {
		IDs, ok := args["<ID>"].([]string)
		if !ok {
			return errors.New("invalid torrent IDs")
		}
		b.torrentIDs, err = strslice.ToIntSlice(IDs)
		if err != nil {
			return errors.New("invalid torrent IDs, must be integers")
		}
	}
	if b.downloadFuse || b.libraryFuse {
		// checking fusermount is available
		_, err := exec.LookPath("fusermount")
		if err != nil {
			return errors.New("fusermount is not available on this system, cannot use the fuse command")
		}

		b.mountPoint = args["<MOUNT_POINT>"].(string)
		if !fs.DirExists(b.mountPoint) {
			return errors.New("fuse mount point does not exist")
		}

		// check it's empty
		if isEmpty, err := fs.DirIsEmpty(b.mountPoint); err != nil {
			return errors.New("could not open Fuse mount point")
		} else if !isEmpty {
			return errors.New("fuse mount point is not empty")
		}
	}
	if b.downloadList {
		state, ok := args["<STATE>"].(string)
		if ok {
			b.downloadState = state
			if !varroa.IsValidDownloadState(b.downloadState) {
				return errors.New("invalid download state, must be among: " + strings.Join(varroa.DownloadFolderStates, ", "))
			}
		}
	}
	if b.snatch {
		b.useFLToken = args["--fl"].(bool)
	}
	if b.checkLog {
		logPath := args["<LOG_FILE>"].(string)
		if !fs.FileExists(logPath) {
			return errors.New("invalid log file, does not exist")
		}
		b.logFile = logPath
	}
	if b.refreshMetadataByID || b.snatch || b.checkLog || b.info || b.reseed {
		b.trackerLabel = args["<TRACKER>"].(string)
	}

	if b.refreshMetadata {
		paths := args["<PATH>"].([]string)
		currentPath, err := os.Getwd()
		if err != nil {
			return err
		}
		for _, p := range paths {
			if !fs.DirExists(p) {
				return errors.New("target path " + p + " does not exist")
			}
			if !varroa.DirectoryContainsMusicAndMetadata(p) {
				return fmt.Errorf(varroa.ErrorFindingMusicAndMetadata, p)
			}
			// find the parent directory
			root := currentPath
			if filepath.IsAbs(p) {
				root = filepath.Dir(p)
			}
			// load metadata
			d := varroa.DownloadEntry{FolderName: p}
			if err := d.Load(root); err != nil {
				return err
			}
			// get tracker + ids
			for i := range d.Tracker {
				b.toRefresh = append(b.toRefresh, refreshTarget{path: p, tracker: d.Tracker[i], id: d.TrackerID[i]})
			}
		}
	}

	// sorting which commands can use the daemon if it's there but should manage if it is not
	b.requiresDaemon = true
	b.canUseDaemon = true
	if b.refreshMetadataByID || b.refreshMetadata || b.snatch || b.checkLog || b.backup || b.stats || b.downloadSearch || b.downloadInfo || b.downloadSort || b.downloadSortID || b.downloadList || b.info || b.downloadClean || b.downloadFuse || b.libraryFuse || b.libraryReorg || b.reseed || b.enhance {
		b.requiresDaemon = false
	}
	// sorting which commands should not interact with the daemon in any case
	if b.refreshMetadata || b.backup || b.showConfig || b.decrypt || b.encrypt || b.downloadSearch || b.downloadInfo || b.downloadSort || b.downloadSortID || b.downloadList || b.downloadClean || b.downloadFuse || b.libraryFuse || b.libraryReorg || b.enhance {
		b.canUseDaemon = false
	}
	return nil
}

func (b *varroaArguments) commandToDaemon() []byte {
	out := varroa.IncomingJSON{Site: b.trackerLabel}
	if b.stats {
		out.Command = "stats"
	}
	if b.stop {
		// to cleanly close the unix socket
		out.Command = "stop"
	}
	if b.uptime {
		out.Command = "uptime"
	}
	if b.status {
		out.Command = "status"
	}
	if b.refreshMetadataByID {
		out.Command = "refresh-metadata-by-id"
		out.Args = intslice.ToStringSlice(b.torrentIDs)
	}
	if b.snatch {
		out.Command = "snatch"
		out.Args = intslice.ToStringSlice(b.torrentIDs)
		out.FLToken = b.useFLToken
	}
	if b.info {
		out.Command = "info"
		out.Args = intslice.ToStringSlice(b.torrentIDs)
	}
	if b.checkLog {
		out.Command = "check-log"
		out.Args = []string{b.logFile}
	}
	if b.reseed {
		out.Command = "reseed"
		out.Args = b.paths
	}
	commandBytes, err := json.Marshal(out)
	if err != nil {
		logthis.Error(errors.Wrap(err, "cannot parse command"), logthis.NORMAL)
		return []byte{}
	}
	return commandBytes
}
