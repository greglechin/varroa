package varroa

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/jasonlvhit/gocron"
	"github.com/mholt/archiver"
	"github.com/pkg/errors"
	daemon "github.com/sevlyar/go-daemon"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/ipc"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/strslice"
	"gitlab.com/catastrophic/assistance/ui"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	archivesDir         = "archives"
	archiveNameTemplate = "varroa_%s.zip"
)

// SendOrders from the CLI to the running daemon
func SendOrders(command []byte) error {
	dcClient := ipc.NewUnixSocketClient(daemonSocket)
	go func() {
		if err := dcClient.RunClient(); err != nil {
			logthis.Error(err, logthis.NORMAL)
		}
	}()
	// goroutine to display anything that is sent back from the daemon
	go func() {
		for {
			a := <-dcClient.Incoming
			if string(a) != ipc.StopCommand {
				fmt.Println(string(a))
			}
		}
	}()
	// waiting for connection to unix domain socket
	<-dcClient.ClientConnected
	// sending command
	dcClient.Outgoing <- command
	// waiting for end of connection
	<-dcClient.ClientDisconnected
	return nil
}

// awaitOrders in the daemon from the CLI
func awaitOrders(e *Environment) {
	go func() {
		if err := e.daemonUnixSocket.RunServer(); err != nil {
			logthis.Error(err, logthis.NORMAL)
		}
	}()
	<-e.daemonUnixSocket.ServerUp

Loop:
	for {
		<-e.daemonUnixSocket.ClientConnected
		// output back things to CLI
		logOutput := logthis.Subscribe()

	Loop2:
		for {
			select {
			case l := <-logOutput:
				e.daemonUnixSocket.Outgoing <- []byte(l.(string))
			case a := <-e.daemonUnixSocket.Incoming:
				orders := IncomingJSON{}
				if jsonErr := json.Unmarshal(a, &orders); jsonErr != nil {
					logthis.Error(errors.Wrap(jsonErr, "Error parsing incoming command from unix socket"), logthis.NORMAL)
					continue
				}
				var t *tracker.Gazelle
				var err error
				if orders.Site != "" {
					t, err = e.Tracker(orders.Site)
					if err != nil {
						logthis.Error(errors.Wrap(err, "Error parsing tracker label for command from unix socket"), logthis.NORMAL)
						continue
					}
				}

				switch orders.Command {
				case "stats":
					if err := GenerateStats(e); err != nil {
						logthis.Error(errors.Wrap(err, ErrorGeneratingGraphs), logthis.NORMAL)
					}
				case "refresh-metadata-by-id":
					if err := RefreshMetadata(e, t, orders.Args); err != nil {
						logthis.Error(errors.Wrap(err, ErrorRefreshingMetadata), logthis.NORMAL)
					}
				case "snatch":
					if err := SnatchTorrents(e, t, orders.Args, orders.FLToken); err != nil {
						logthis.Error(errors.Wrap(err, ErrorSnatchingTorrent), logthis.NORMAL)
					}
				case "info":
					if err := ShowTorrentInfo(e, t, orders.Args); err != nil {
						logthis.Error(errors.Wrap(err, ErrorShowingTorrentInfo), logthis.NORMAL)
					}
				case "check-log":
					if err := CheckLog(t, orders.Args); err != nil {
						logthis.Error(errors.Wrap(err, ErrorCheckingLog), logthis.NORMAL)
					}
				case "uptime":
					if e.startTime.IsZero() {
						logthis.Info("Daemon is not running.", logthis.NORMAL)
					} else {
						logthis.Info("varroa musica daemon up for "+time.Since(e.startTime).String()+".", logthis.NORMAL)
					}
				case "status":
					if e.startTime.IsZero() {
						logthis.Info("Daemon is not running.", logthis.NORMAL)
					} else {
						logthis.Info(statusString(e), logthis.NORMAL)
					}
				case "reseed":
					if err := Reseed(t, orders.Args); err != nil {
						logthis.Error(errors.Wrap(err, ErrorReseed), logthis.NORMAL)
					}
				case ipc.StopCommand:
					logthis.Info("Stopping daemon...", logthis.NORMAL)
					break Loop
				}
				e.daemonUnixSocket.Outgoing <- []byte(ipc.StopCommand)
			case <-e.daemonUnixSocket.ClientDisconnected:
				// stop output back things to CLI
				logthis.Unsubscribe(logOutput)
				break Loop2
			}
		}
	}
	e.daemonUnixSocket.StopCurrent()
}

func statusString(e *Environment) string {
	// version
	status := fmt.Sprintf(FullVersion+"\n", FullName, Version)
	// uptime
	status += "Daemon up since " + e.startTime.Format("2006.01.02 15h04") + " (uptime: " + time.Since(e.startTime).String() + ").\n"
	// autosnatch enabled?
	conf, err := NewConfig(DefaultConfigurationFile)
	if err == nil {
		for _, as := range conf.Autosnatch {
			status += "Autosnatching for tracker " + as.Tracker + ": "
			if as.disabledAutosnatching {
				status += "disabled!\n"
			} else {
				status += "enabled.\n"
			}
		}
	}

	// TODO last autosnatched release for tracker X: date
	return status
}

// GenerateStats for all labels and the associated HTML index.
func GenerateStats(e *Environment) error {
	atLeastOneError := false
	stats, err := NewStatsDB(filepath.Join(StatsDir, DefaultHistoryDB))
	if err != nil {
		return errors.Wrap(err, "could not access the stats database")
	}
	if err := stats.Update(); err != nil {
		return errors.Wrap(err, "error updating database")
	}

	// get tracker labels from config.
	config, configErr := NewConfig(DefaultConfigurationFile)
	if configErr != nil {
		return configErr
	}
	// generate graphs
	for _, tracker := range config.TrackerLabels() {
		if err := stats.GenerateAllGraphsForTracker(tracker); err != nil {
			logthis.Error(err, logthis.NORMAL)
			atLeastOneError = true
		}
	}

	// generate index.html
	if err := e.GenerateIndex(); err != nil {
		logthis.Error(errors.Wrap(err, "Error generating index.html"), logthis.NORMAL)
	}
	if atLeastOneError {
		return errors.New(ErrorGeneratingGraphs)
	}
	return nil
}

// RefreshMetadata for a list of releases on a tracker
func RefreshMetadata(e *Environment, t *tracker.Gazelle, IDStrings []string) error {
	if len(IDStrings) == 0 {
		return errors.New("Error: no ID provided")
	}

	stats, err := NewStatsDB(filepath.Join(StatsDir, DefaultHistoryDB))
	if err != nil {
		return errors.Wrap(err, "could not access the stats database")
	}

	for _, id := range IDStrings {
		var found Release
		info := &TrackerMetadata{}
		findIDsQuery := q.And(q.Eq("Tracker", t.Name), q.Eq("TorrentID", id))
		if err := stats.db.DB.Select(findIDsQuery).First(&found); err != nil {
			if err == storm.ErrNotFound {
				// not found, try to locate download directory nonetheless
				if e.config.DownloadFolderConfigured {
					logthis.Info("Release not found in history, trying to locate in downloads directory.", logthis.NORMAL)
					// get data from tracker
					if err := info.LoadFromID(t, id); err != nil {
						logthis.Error(errors.Wrap(err, errorCouldNotGetTorrentInfo), logthis.NORMAL)
						break
					}
					fullFolder := filepath.Join(e.config.General.DownloadDir, info.FolderName)
					if fs.DirExists(fullFolder) {
						if daemon.WasReborn() {
							go info.SaveFromTracker(fullFolder, t)
						} else {
							info.SaveFromTracker(fullFolder, t)
						}
					} else {
						logthis.Info(fmt.Sprintf(errorCannotFindID, id), logthis.NORMAL)
					}

				} else {
					logthis.Info(fmt.Sprintf(errorCannotFindID, id), logthis.NORMAL)
					continue
				}
			} else {
				logthis.Error(errors.Wrap(err, errorCouldNotGetTorrentInfo), logthis.NORMAL)
				continue
			}
		} else {
			// was found
			logthis.Info("Found release with ID "+found.TorrentID+" in history: "+found.ShortString()+". Getting tracker metadata.", logthis.NORMAL)
			// get data from tracker
			if err := info.LoadFromID(t, found.TorrentID); err != nil {
				logthis.Error(errors.Wrap(err, errorCouldNotGetTorrentInfo), logthis.NORMAL)
				break
			}
			fullFolder := filepath.Join(e.config.General.DownloadDir, info.FolderName)
			if daemon.WasReborn() {
				go info.SaveFromTracker(fullFolder, t)
			} else {
				info.SaveFromTracker(fullFolder, t)
			}
		}
		// check the number of active seeders
		if !info.IsWellSeeded() {
			logthis.Info(ui.Red("This torrent has less than "+strconv.Itoa(minimumSeeders)+" seeders; if that is not already the case, consider reseeding it."), logthis.NORMAL)
		}
		// if release is reported, warn and offer link.
		if info.Reported {
			logthis.Info(ui.Red("This torrent has been reported. For more information, see: "+info.ReleaseURL), logthis.NORMAL)
		}

	}
	return nil
}

// RefreshLibraryMetadata for a list of releases on a tracker, using the given location instead of assuming they are in the download directory.
func RefreshLibraryMetadata(path string, t *tracker.Gazelle, id string) error {
	if !DirectoryContainsMusicAndMetadata(path) {
		return fmt.Errorf(ErrorFindingMusicAndMetadata, path)
	}
	// get data from tracker
	info := &TrackerMetadata{}
	if err := info.LoadFromID(t, id); err != nil {
		return errors.Wrap(err, errorCouldNotGetTorrentInfo)
	}
	return info.SaveFromTracker(path, t)
}

// SnatchTorrents on a tracker using their TorrentIDs
func SnatchTorrents(e *Environment, t *tracker.Gazelle, IDStrings []string, useFLToken bool) error {
	if len(IDStrings) == 0 {
		return errors.New("Error: no ID provided")
	}
	// snatch
	for _, id := range IDStrings {
		release, err := manualSnatchFromID(e, t, id, useFLToken)
		if err != nil {
			return errors.New("Error snatching torrent with ID #" + id)
		}
		logthis.Info("Successfully snatched torrent "+release.ShortString(), logthis.NORMAL)
	}
	return nil
}

// ShowTorrentInfo of a list of releases on a tracker
func ShowTorrentInfo(e *Environment, t *tracker.Gazelle, IDStrings []string) error {
	if len(IDStrings) == 0 {
		return errors.New("Error: no ID provided")
	}

	stats, err := NewStatsDB(filepath.Join(StatsDir, DefaultHistoryDB))
	if err != nil {
		return errors.Wrap(err, "could not access the stats database")
	}

	// get info
	for _, id := range IDStrings {
		logthis.Info(fmt.Sprintf("+ Info about %s / %s: \n", t.Name, id), logthis.NORMAL)
		// get release info from ID
		info := &TrackerMetadata{}
		if err := info.LoadFromID(t, id); err != nil {
			logthis.Error(errors.Wrap(err, fmt.Sprintf("Could not get info about torrent %s on %s, may not exist", id, t.Name)), logthis.NORMAL)
			continue
		}
		release := info.Release()
		logthis.Info(info.TextDescription(true)+"\n", logthis.NORMAL)

		// find if in history
		var found Release
		if selectErr := stats.db.DB.Select(q.And(q.Eq("Tracker", t.Name), q.Eq("TorrentID", id))).First(&found); selectErr != nil {
			logthis.Info("+ This torrent has not been snatched with varroa.", logthis.NORMAL)
		} else {
			logthis.Info("+ This torrent has been snatched with varroa.", logthis.NORMAL)
		}

		// checking the files are still there (if snatched with or without varroa)
		if e.config.DownloadFolderConfigured {
			releaseFolder := filepath.Join(e.config.General.DownloadDir, info.FolderName)
			if fs.DirExists(releaseFolder) {
				logthis.Info(fmt.Sprintf("Files seem to still be in the download directory: %s", releaseFolder), logthis.NORMAL)
				// TODO maybe display when the metadata was last updated?
			} else {
				logthis.Info("The files could not be found in the download directory.", logthis.NORMAL)
			}
		}

		// check and print if info/release triggers filters
		autosnatchConfig, err := e.config.GetAutosnatch(t.Name)
		if err != nil {
			logthis.Info("Cannot find autosnatch configuration for tracker "+t.Name, logthis.NORMAL)
		} else {
			logthis.Info("+ Showing autosnatch filters results for this release:\n", logthis.NORMAL)
			for _, filter := range e.config.Filters {
				// checking if filter is specifically set for this tracker (if nothing is indicated, all trackers match)
				if len(filter.Tracker) != 0 && !strslice.Contains(filter.Tracker, t.Name) {
					logthis.Info(fmt.Sprintf(infoFilterIgnoredForTracker, filter.Name, t.Name), logthis.NORMAL)
					continue
				}
				// checking if a filter is triggered
				if release.Satisfies(filter) && release.HasCompatibleTrackerInfo(filter, autosnatchConfig.BlacklistedUploaders, info) {
					// checking if duplicate
					if !filter.AllowDuplicates && stats.AlreadySnatchedDuplicate(release) {
						logthis.Info(filter.Name+": "+infoNotSnatchingDuplicate, logthis.NORMAL)
						continue
					}
					// checking if a torrent from the same group has already been downloaded
					if filter.UniqueInGroup && stats.AlreadySnatchedFromGroup(release) {
						logthis.Info(filter.Name+": "+infoNotSnatchingUniqueInGroup, logthis.NORMAL)
						continue
					}
					logthis.Info(fmt.Sprintf(infoFilterTriggered, filter.Name), logthis.NORMAL)
				}
			}
		}
	}
	return nil
}

// Reseed a release using local files and tracker metadata
func Reseed(t *tracker.Gazelle, path []string) error {
	// get config.
	conf, configErr := NewConfig(DefaultConfigurationFile)
	if configErr != nil {
		return configErr
	}
	if !conf.DownloadFolderConfigured {
		return errors.New("impossible to reseed release if downloads directory is not configured")
	}
	// parse metadata for tracker, and get tid
	// assuming reseeding one at a time only (as limited by CLI)
	toc := TrackerOriginJSON{Path: filepath.Join(path[0], MetadataDir, OriginJSONFile)}
	if err := toc.Load(); err != nil {
		return errors.Wrap(err, "error reading origin.json")
	}
	// check that tracker is in list of origins
	oj, ok := toc.Origins[t.Name]
	if !ok {
		return errors.New("release does not originate from tracker " + t.Name)
	}

	// copy files if necessary
	// if the relative path of the downloads directory and the release path is the folder name, it means the path is
	// directly inside the downloads directory, where we want it to reseed.
	// if it is not, we need to copy the files.
	// TODO: maybe hard link instead if in the same filesystem
	// TODO : deal with more than one path
	rel, err := filepath.Rel(conf.General.DownloadDir, path[0])
	if err != nil {
		return errors.Wrap(err, "error trying to locate the target path relatively to the downloads directory")
	}
	// copy files if not in downloads directory
	if rel != filepath.Base(path[0]) {
		if err := fs.CopyDir(path[0], filepath.Join(conf.General.DownloadDir, filepath.Base(path[0])), false); err != nil {
			return errors.Wrap(err, "error copying files to downloads directory")
		}
		logthis.Info("Release files have been copied inside the downloads directory", logthis.NORMAL)
	}

	// TODO TO A TEMP DIR, then compare torrent description with path contents; if OK only copy .torrent to conf.General.WatchDir
	// downloading torrent
	if err := t.DownloadTorrentFromID(strconv.Itoa(oj.ID), conf.General.WatchDir, false); err != nil {
		return errors.Wrap(err, "error downloading torrent file")
	}
	logthis.Info("Torrent downloaded, your bittorrent client should be able to reseed the release.", logthis.NORMAL)
	return nil
}

// CheckLog on a tracker's logchecker
func CheckLog(t *tracker.Gazelle, logPaths []string) error {
	for _, log := range logPaths {
		score, err := t.GetLogScore(log)
		if err != nil {
			return errors.Wrap(err, errorGettingLogScore)
		}
		logthis.Info(fmt.Sprintf("Logchecker results: %s.", score), logthis.NORMAL)
	}
	return nil
}

// ArchiveUserFiles in a timestamped compressed archive.
func ArchiveUserFiles() error {
	// generate Timestamp
	timestamp := time.Now().Format("2006-01-02_15h04m05s")
	archiveName := fmt.Sprintf(archiveNameTemplate, timestamp)
	if !fs.DirExists(archivesDir) {
		if err := os.MkdirAll(archivesDir, 0755); err != nil {
			logthis.Error(errors.Wrap(err, errorArchiving), logthis.NORMAL)
			return errors.Wrap(err, errorArchiving)
		}
	}
	var backupFiles []string
	// find all .db files, save them along with the configuration file
	f, err := os.Open(StatsDir)
	if err != nil {
		return errors.Wrap(err, "Error opening "+StatsDir)
	}
	contents, err := f.Readdirnames(-1)
	if err != nil {
		return errors.Wrap(err, "Error reading directory "+StatsDir)
	}
	f.Close()
	for _, c := range contents {
		if filepath.Ext(c) == msgpackExt {
			backupFiles = append(backupFiles, filepath.Join(StatsDir, c))
		}
	}
	// backup the configuration file
	if fs.FileExists(DefaultConfigurationFile) {
		backupFiles = append(backupFiles, DefaultConfigurationFile)
	}
	encryptedConfigurationFile := strings.TrimSuffix(DefaultConfigurationFile, yamlExt) + encryptedExt
	if fs.FileExists(encryptedConfigurationFile) {
		backupFiles = append(backupFiles, encryptedConfigurationFile)
	}
	// generate archive
	err = archiver.Archive(backupFiles, filepath.Join(archivesDir, archiveName))
	if err != nil {
		logthis.Error(errors.Wrap(err, errorArchiving), logthis.NORMAL)
	}
	return err
}

// parseQuota output to find out what remains available
func parseQuota(cmdOut string) (float32, int64, error) {
	output := strings.TrimSpace(cmdOut)
	if output == "" {
		return -1, -1, errors.New("no quota defined for user")
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		return -1, -1, errors.New("unexpected quota output")
	}
	var relevantParts []string
	for _, p := range strings.Split(lines[2], " ") {
		if strings.TrimSpace(p) != "" {
			relevantParts = append(relevantParts, p)
		}
	}
	used, err := strconv.Atoi(relevantParts[1])
	if err != nil {
		return -1, -1, errors.New("error parsing quota output")
	}
	quota, err := strconv.Atoi(relevantParts[2])
	if err != nil {
		return -1, -1, errors.New("error parsing quota output")
	}
	// assuming blocks of 1kb
	return 100 * float32(used) / float32(quota), int64(quota-used) * 1024, nil
}

// checkQuota on the machine the daemon is run
func checkQuota(e *Environment) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	// parse quota -u $(whoami)
	cmdOut, err := exec.Command("quota", "-u", u.Username, "-w").Output()
	if err != nil {
		return err
	}
	pc, remaining, err := parseQuota(string(cmdOut))
	if err != nil {
		return err
	}
	logthis.Info(fmt.Sprintf(currentUsage, pc, fs.FileSizeDelta(remaining)), logthis.NORMAL)
	// send warning if this is worrying
	if pc >= 98 {
		logthis.Info(veryLowDiskSpace, logthis.NORMAL)
		return Notify(veryLowDiskSpace, FullName, "info", e)
	} else if pc >= 95 {
		logthis.Info(lowDiskSpace, logthis.NORMAL)
		return Notify(lowDiskSpace, FullName, "info", e)
	}
	return nil
}

// checkFreeDiskSpace based on the main download directory's location.
func checkFreeDiskSpace(e *Environment) error {
	// get config.
	conf, configErr := NewConfig(DefaultConfigurationFile)
	if configErr != nil {
		return configErr
	}
	if conf.DownloadFolderConfigured {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(conf.General.DownloadDir, &stat); err != nil {
			return errors.Wrap(err, "error finding free disk space")
		}
		// Available blocks * size per block = available space in bytes
		freeBytes := stat.Bavail * uint64(stat.Bsize)
		allBytes := stat.Blocks * uint64(stat.Bsize)
		pcRemaining := 100 * float32(freeBytes) / float32(allBytes)
		// send warning if this is worrying
		if pcRemaining <= 2 {
			logthis.Info(veryLowDiskSpace, logthis.NORMAL)
			return Notify(veryLowDiskSpace, FullName, "info", e)
		} else if pcRemaining <= 5 {
			logthis.Info(lowDiskSpace, logthis.NORMAL)
			return Notify(lowDiskSpace, FullName, "info", e)
		}
		return nil
	}
	return errors.New("download directory not configured, cannot check free disk space")
}

// automatedTasks is a list of cronjobs for maintenance, backup, or non-critical operations
func automatedTasks(e *Environment) {
	// new scheduler
	s := gocron.NewScheduler()

	// 1. every day, backup user files
	s.Every(1).Day().At("00:00").Do(ArchiveUserFiles)
	// 2. a little later, also compress the git repository if gitlab pages are configured
	if e.config.gitlabPagesConfigured {
		s.Every(7).Day().At("00:15").Do(e.git.Compress)
	}
	// 3. check quota is available
	_, err := exec.LookPath("quota")
	if err != nil {
		logthis.Info("The command 'quota' is not available on this system, not able to check disk quota", logthis.NORMAL)
	} else {
		// first check
		if err := checkQuota(e); err != nil {
			logthis.Error(errors.Wrap(err, "error checking user quota: quota usage monitoring off"), logthis.NORMAL)
		} else {
			// scheduler for subsequent quota checks
			s.Every(1).Hour().Do(checkQuota)
		}
	}
	// 4. check disk space is available
	// first check
	if err := checkFreeDiskSpace(e); err != nil {
		logthis.Error(errors.Wrap(err, "error checking free disk space: disk usage monitoring off"), logthis.NORMAL)
	} else {
		// scheduler for subsequent quota checks
		s.Every(1).Hour().Do(checkFreeDiskSpace)
	}
	// 5. update database stats
	s.Every(1).Day().At("00:05").Do(GenerateStats, e)
	// launch scheduler
	<-s.Start()
}
