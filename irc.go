package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/thoj/go-ircevent"
)

const (
	announcePattern = `(.*?) - (.*) \[([\d]{4})\] \[(Album|Soundtrack|Compilation|Anthology|EP|Single|Live album|Remix|Bootleg|Interview|Mixtape|Demo|Concert Recording|DJ Mix|Unknown)\] - (FLAC|MP3|AAC) / (Lossless|24bit Lossless|V0 \(VBR\)|V2 \(VBR\)|320|256) /( (Log) /)?( (-*\d+)\% /)?( (Cue) /)? (CD|DVD|Vinyl|Soundboard|SACD|DAT|Cassette|WEB|Blu-Ray) (/ (Scene) )?- (http[s]?://[\w\./:]*torrents\.php\?id=[\d]*) / (http[s]?://[\w\./:]*torrents\.php\?action=download&id=[\d]*) - ([\w\., ]*)`

	errorDealingWithAnnounce     = "Error dealing with announced torrent: "
	errorConnectingToIRC         = "Error connecting to IRC: "
	errorCouldNotGetTorrentInfo  = "Error retreiving torrent info from tracker"
	errorCouldNotMoveTorrent     = "Error moving torrent to destination folder: "
	errorDownloadingTorrent      = "Error downloading torrent: "
	errorRemovingTempFile        = "Error removing temporary file %s"
	errorAddingToHistory         = "Error adding release to history"
	errorWaitingForDownload      = "Error waiting for download folder to be created"
	errorWritingJSONMetadata     = "Error writing metadata file: "
	errorDownloadingTrackerCover = "Error downloading tracker cover: "

	notSnatchingDuplicate = "Similar release already downloaded, and duplicates are not allowed"
	metadataSaved         = "Metadata saved to: "
	coverSaved            = "Cover saved to: "
	trackerMetadataFile   = "tracker_metadata.json"
	trackerCoverFile      = "tracker_cover"

	timeoutDownloadFolderCreation = 100
)

func waitUntilExists(path string) error {
	cpt := 0
	var err error
	keepScanning := true
	for keepScanning {
		if !DirectoryExists(path) {
			if cpt < timeoutDownloadFolderCreation {
				time.Sleep(1 * time.Second)
				cpt++
			} else {
				err = errors.New(errorWaitingForDownload)
				keepScanning = false
			}
		} else {
			err = nil
			keepScanning = false
		}
	}
	return err
}

func saveTrackerMetadata(info *TrackerTorrentInfo) {
	if !conf.downloadFolderConfigured() {
		return
	}
	go func() {
		completePath := filepath.Join(conf.downloadFolder, info.folder)
		if err := waitUntilExists(completePath); err != nil {
			logThis(err.Error(), VERBOSE)
			return
		}
		// write tracker metadata to target folder
		if err := ioutil.WriteFile(filepath.Join(completePath, trackerMetadataFile), info.fullJSON, 0644); err != nil {
			logThis(errorWritingJSONMetadata+err.Error(), NORMAL)
		} else {
			logThis(metadataSaved+info.folder, VERBOSE)
		}
		// download tracker cover to target folder
		if err := info.DownloadCover(filepath.Join(completePath, trackerCoverFile)); err != nil {
			logThis(errorDownloadingTrackerCover+err.Error(), NORMAL)
		} else {
			logThis(coverSaved+info.folder, VERBOSE)
		}
	}()
}

func AnalyzeAnnounce(announced string, tracker *GazelleTracker) (*Release, error) {
	// getting information
	r := regexp.MustCompile(announcePattern)
	hits := r.FindAllStringSubmatch(announced, -1)
	if len(hits) != 0 {
		release, err := NewRelease(hits[0])
		if err != nil {
			return nil, err
		}
		logThis(release.String(), VERBOSEST)

		// if satisfies a filter, download
		var downloadedInfo bool
		var downloadedTorrent bool
		var info *TrackerTorrentInfo
		for _, filter := range conf.filters {
			// checking if duplicate
			if !filter.allowDuplicate && history.HasDupe(release) {
				logThis(notSnatchingDuplicate, VERBOSE)
				continue
			}
			// checking if a filter is triggered
			if release.Satisfies(filter) {
				// get torrent info!
				if !downloadedInfo {
					info, err = tracker.GetTorrentInfo(release.torrentID)
					if err != nil {
						return nil, errors.New(errorCouldNotGetTorrentInfo)
					}
					downloadedInfo = true
					logThis(info.String(), VERBOSE)
				}
				// else check other criteria
				if release.HasCompatibleTrackerInfo(filter, conf.blacklistedUploaders, info) {
					logThis(" -> "+release.ShortString()+" triggered filter "+filter.label+", snatching.", NORMAL)
					if _, err := tracker.Download(release); err != nil {
						return nil, errors.New(errorDownloadingTorrent + err.Error())
					}
					downloadedTorrent = true
					// move to relevant watch directory
					destination := conf.defaultDestinationFolder
					if filter.destinationFolder != "" {
						destination = filter.destinationFolder
					}
					if err := CopyFile(release.filename, filepath.Join(destination, release.filename)); err != nil {
						return nil, errors.New(errorCouldNotMoveTorrent + err.Error())
					}
					// adding to history
					if err := history.SnatchHistory.Add(release, filter.label); err != nil {
						logThis(errorAddingToHistory, NORMAL)
					}
					// send notification
					if err := notification.Send(filter.label + ": Snatched " + release.ShortString()); err != nil {
						logThis(errorNotification+err.Error(), VERBOSE)
					}
					// save metadata once the download folder is created
					saveTrackerMetadata(info)
					// no need to consider other filters
					break
				}
			}
		}
		// if torrent was downloaded, remove temp copy
		if downloadedTorrent {
			if err := os.Remove(release.filename); err != nil {
				logThis(fmt.Sprintf(errorRemovingTempFile, release.filename), VERBOSE)
			}
			return release, nil
		}
		logThis("No filter is interested in that release. Ignoring.", VERBOSE)
		return nil, nil
	}
	return nil, errors.New("No hits!")
}

func ircHandler() {
	IRCClient := irc.IRC(conf.botName, conf.user)
	IRCClient.UseTLS = conf.ircSSL
	IRCClient.TLSConfig = &tls.Config{InsecureSkipVerify: conf.ircSSLSkipVerify}
	IRCClient.AddCallback("001", func(e *irc.Event) {
		IRCClient.Privmsg("NickServ", "IDENTIFY "+conf.nickServPassword)
		IRCClient.Privmsg(conf.announcer, fmt.Sprintf("enter %s %s %s", conf.announceChannel, conf.user, conf.ircKey))
	})
	IRCClient.AddCallback("PRIVMSG", func(e *irc.Event) {
		if e.Nick != conf.announcer {
			return // spam
		}
		// e.Arguments's first element is the message's recipient, the second is the actual message
		switch e.Arguments[0] {
		case conf.botName:
			// if sent to the bot, it's now ok to join the announce channel
			// waiting for the announcer bot to actually invite us
			time.Sleep(100 * time.Millisecond)
			IRCClient.Join(conf.announceChannel)
		case conf.announceChannel:
			// if sent to the announce channel, it's a new release
			if !disabledAutosnatching {
				announced := e.Message()
				logThis("++ Announced: "+announced, VERBOSE)
				if _, err := AnalyzeAnnounce(announced, tracker); err != nil {
					logThis(errorDealingWithAnnounce+err.Error(), VERBOSE)
					return
				}
			}
		}
	})
	err := IRCClient.Connect(conf.ircServer)
	if err != nil {
		logThis(errorConnectingToIRC+err.Error(), NORMAL)
		return
	}
	IRCClient.Loop()
}
