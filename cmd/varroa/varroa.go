package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/intslice"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/ui"
	"gitlab.com/passelecasque/varroa"
)

func main() {
	env := varroa.NewEnvironment()

	// parsing CLI
	cli := &varroaArguments{}
	if err := cli.parseCLI(os.Args[1:]); err != nil {
		logthis.Error(errors.Wrap(err, varroa.ErrorArguments), logthis.NORMAL)
		return
	}
	if cli.builtin {
		return
	}

	// prepare cleanup
	defer closeDB()

	// here commands that have no use for the daemon
	if !cli.canUseDaemon {
		if cli.backup {
			if err := varroa.ArchiveUserFiles(); err == nil {
				logthis.Info(varroa.InfoUserFilesArchived, logthis.NORMAL)
			}
			return
		}
		// loading configuration
		config, err := varroa.NewConfig(varroa.DefaultConfigurationFile)
		if err != nil {
			logthis.Error(errors.Wrap(err, varroa.ErrorLoadingConfig), logthis.NORMAL)
			return
		}
		env.SetConfig(config)

		if cli.encrypt || cli.decrypt {
			// now dealing with encrypt/decrypt commands, which both require the passphrase from user
			passphrase, err := varroa.GetPassphrase()
			if err != nil {
				logthis.Error(errors.Wrap(err, "Error getting passphrase"), logthis.NORMAL)
			}
			passphraseBytes := make([]byte, 32)
			copy(passphraseBytes[:], passphrase)
			if cli.encrypt {
				if err = config.Encrypt(varroa.DefaultConfigurationFile, passphraseBytes); err != nil {
					logthis.Info(err.Error(), logthis.NORMAL)
					return
				}
				logthis.Info(varroa.InfoEncrypted, logthis.NORMAL)
			}
			if cli.decrypt {
				if err = config.DecryptTo(varroa.DefaultConfigurationFile, passphraseBytes); err != nil {
					logthis.Error(err, logthis.NORMAL)
					return
				}
				logthis.Info(varroa.InfoDecrypted, logthis.NORMAL)
			}
			return
		}
		if cli.showConfig {
			fmt.Print("Found in configuration file: \n\n")
			fmt.Println(config)
			return
		}
		if cli.enhance {
			enh, err := varroa.NewReleaseDir(cli.paths[0])
			if err != nil {
				logthis.Error(err, logthis.NORMAL)
				return
			}
			if err := enh.Enhance(); err != nil {
				logthis.Error(err, logthis.NORMAL)
			}
			return
		}
		if cli.downloadSearch || cli.downloadInfo || cli.downloadSort || cli.downloadSortID || cli.downloadList || cli.downloadClean {
			if !config.DownloadFolderConfigured {
				logthis.Error(errors.New("Cannot scan for downloads, downloads folder not configured"), logthis.NORMAL)
				return
			}
			var additionalSources []string
			if config.LibraryConfigured {
				additionalSources = config.Library.AdditionalSources
			}
			downloads, err := varroa.NewDownloadsDB(varroa.DefaultDownloadsDB, config.General.DownloadDir, additionalSources)
			if err != nil {
				logthis.Error(err, logthis.NORMAL)
				return
			}
			defer downloads.Close()

			// simple operation, only requires access to download folder, since it will clean unindexed folders
			if cli.downloadClean {
				if err = downloads.Clean(); err != nil {
					logthis.Error(err, logthis.NORMAL)
				} else {
					fmt.Println("Downloads directory cleaned of empty folders & folders containing only tracker metadata.")
				}
				return
			}
			if cli.downloadSort || cli.downloadSortID {
				// setting up to load history, etc.
				if err = env.SetUp(false); err != nil {
					logthis.Error(errors.Wrap(err, varroa.ErrorSettingUp), logthis.NORMAL)
					return
				}
				if !config.LibraryConfigured {
					logthis.Error(errors.New("Cannot sort downloads, library is not configured"), logthis.NORMAL)
					return
				}
				// if no argument, sort everything
				if (cli.downloadSortID && len(cli.torrentIDs) == 0) || (cli.downloadSort && len(cli.paths) == 0) {
					// scanning
					fmt.Println(ui.Green("Scanning downloads for new releases and updated metadata."))
					if err = downloads.Scan(); err != nil {
						logthis.Error(err, logthis.NORMAL)
						return
					}
					defer downloads.Close()
					fmt.Println("Considering new or unsorted downloads.")
					if err = downloads.Sort(env); err != nil {
						logthis.Error(errors.Wrap(err, "Error sorting downloads"), logthis.NORMAL)
						return
					}
					return
				}
				if cli.downloadSort {
					// scanning
					fmt.Println(ui.Green("Scanning downloads for updated metadata."))
					for _, p := range cli.paths {
						if err = downloads.RescanPath(p); err != nil {
							logthis.Error(err, logthis.NORMAL)
							return
						}
						dl, err := downloads.FindByFolderName(p)
						if err != nil {
							logthis.Error(errors.Wrap(err, "error looking for "), logthis.NORMAL)
							return
						}
						cli.torrentIDs = append(cli.torrentIDs, dl.ID)
					}
				} else {
					// scanning
					fmt.Println(ui.Green("Scanning downloads for updated metadata."))
					if err = downloads.RescanIDs(cli.torrentIDs); err != nil {
						logthis.Error(err, logthis.NORMAL)
						return
					}
				}
				fmt.Println("Sorting specific download folders.")
				for _, id := range cli.torrentIDs {
					if err = downloads.SortThisID(env, id, cli.ignoreSorted); err != nil {
						logthis.Error(err, logthis.NORMAL)
					}
				}
				return
			}

			// all subsequent commands require scanning
			fmt.Println(ui.Green("Scanning downloads for new releases and updated metadata."))
			if err = downloads.Scan(); err != nil {
				logthis.Error(err, logthis.NORMAL)
				return
			}
			defer downloads.Close()

			if cli.downloadSearch {
				hits := downloads.FindByArtist(cli.artistName)
				if len(hits) == 0 {
					fmt.Println("Nothing found.")
				} else {
					for _, dl := range hits {
						fmt.Println(dl.ShortString())
					}
				}
				return
			}
			if cli.downloadList {
				if cli.downloadState == "" {
					fmt.Println(downloads.String())
				} else {
					hits := downloads.FindByState(cli.downloadState)
					if len(hits) == 0 {
						fmt.Println("Nothing found.")
					} else {
						for _, dl := range hits {
							fmt.Println(dl.ShortString())
						}
					}
				}
				return
			}
			if cli.downloadInfo {
				dl, err := downloads.FindByID(cli.torrentIDs[0])
				if err != nil {
					logthis.Error(errors.Wrap(err, "Error finding such an ID in the downloads database"), logthis.NORMAL)
					return
				}
				fmt.Println(dl.Description(config.General.DownloadDir))
				return
			}
		}
		if cli.libraryReorg {
			if !config.LibraryConfigured {
				logthis.Info("Library is not configured, missing relevant configuration section.", logthis.NORMAL)
				return
			}
			logthis.Info("Reorganizing releases in the library directory. ", logthis.NORMAL)
			if cli.libraryReorgSimulate {
				fmt.Println(ui.Green("This will simulate the library reorganization, applying the library folder template to all releases, using known tracker metadata. Nothing will actually be renamed or moved."))
			} else {
				fmt.Println(ui.Green("This will apply the library folder template to all releases, using known tracker metadata. It will overwrite any specific name that may have been set manually."))
			}
			if ui.Accept("Confirm") {
				if err = varroa.ReorganizeLibrary(cli.libraryReorgSimulate, cli.libraryReorgInteractive); err != nil {
					logthis.Error(err, logthis.NORMAL)
				}
			}
			return
		}
		// using stormDB
		if cli.downloadFuse {
			logthis.Info("Mounting FUSE filesystem in "+cli.mountPoint, logthis.NORMAL)
			if err = varroa.FuseMount(config.General.DownloadDir, cli.mountPoint, varroa.DefaultDownloadsDB); err != nil {
				logthis.Error(err, logthis.NORMAL)
				return
			}
			logthis.Info("Unmounting FUSE filesystem, fusermount -u has presumably been called.", logthis.VERBOSE)
			return
		}
		if cli.libraryFuse {
			if !config.LibraryConfigured {
				logthis.Info("Cannot mount FUSE filesystem for the library, missing relevant configuration section.", logthis.NORMAL)
				return
			}
			logthis.Info("Mounting FUSE filesystem in "+cli.mountPoint, logthis.NORMAL)
			if err = varroa.FuseMount(config.Library.Directory, cli.mountPoint, varroa.DefaultLibraryDB); err != nil {
				logthis.Error(err, logthis.NORMAL)
				return
			}
			logthis.Info("Unmounting FUSE filesystem, fusermount -u has presumably been called.", logthis.VERBOSE)
			return
		}
	}

	// loading configuration
	if err := env.LoadConfiguration(); err != nil {
		fmt.Println(errors.Wrap(err, varroa.ErrorLoadingConfig).Error())
		return
	}

	d := varroa.NewDaemon()
	if cli.start {
		// launching daemon
		if !cli.noDaemon {
			// daemonizing process
			if err := d.Start(os.Args); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorGettingDaemonContext), logthis.NORMAL)
				return
			}
			// if not in daemon, job is over; exiting.
			// the spawned daemon will continue.
			if !d.IsRunning() {
				return
			}
		}
		// setting up for the daemon or main process
		if err := env.SetUp(true); err != nil {
			logthis.Error(errors.Wrap(err, varroa.ErrorSettingUp), logthis.NORMAL)
			return
		}
		// launch goroutines
		varroa.GoGoRoutines(env, cli.noDaemon)

		if !cli.noDaemon {
			// wait until daemon is stopped.
			d.WaitForStop()
		} else {
			// wait for ^C to quit.
			fmt.Println(ui.Red("Running in no-daemon mode. Ctrl+C to quit."))
			c := make(chan os.Signal)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			// waiting...
			<-c
			fmt.Println(ui.Red("Terminating."))
		}

		if err := varroa.Notify("Stopping varroa!", varroa.FullName, "info", env); err != nil {
			logthis.Error(err, logthis.NORMAL)
		}
		return
	}

	// at this point commands either require the daemon or can use it
	// assessing if daemon is running
	daemonProcess, err := d.Find()
	if err != nil {
		// no daemon found, running commands directly.
		if cli.requiresDaemon {
			logthis.Error(errors.Wrap(err, varroa.ErrorFindingDaemon), logthis.NORMAL)
			fmt.Println(varroa.InfoUsage)
			return
		}
		// setting up since the daemon isn't running
		if err := env.SetUp(false); err != nil {
			logthis.Error(errors.Wrap(err, varroa.ErrorSettingUp), logthis.NORMAL)
			return
		}
		// general commands
		if cli.stats {
			if err := varroa.GenerateStats(env); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorGeneratingGraphs), logthis.NORMAL)
			}
			return
		}
		if cli.refreshMetadata {
			for _, r := range cli.toRefresh {
				tracker, err := env.Tracker(r.tracker)
				if err != nil {
					logthis.Info(fmt.Sprintf("Tracker %s not defined in configuration file", cli.trackerLabel), logthis.NORMAL)
					return
				}
				if err = varroa.RefreshLibraryMetadata(r.path, tracker, strconv.Itoa(r.id)); err != nil {
					logthis.Error(errors.Wrap(err, varroa.ErrorRefreshingMetadata), logthis.NORMAL)
				}
			}
			return
		}

		// commands that require tracker label
		tracker, err := env.Tracker(cli.trackerLabel)
		if err != nil {
			logthis.Info(fmt.Sprintf("Tracker %s not defined in configuration file", cli.trackerLabel), logthis.NORMAL)
			return
		}
		if cli.refreshMetadataByID {
			if err := varroa.RefreshMetadata(env, tracker, intslice.ToStringSlice(cli.torrentIDs)); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorRefreshingMetadata), logthis.NORMAL)
			}
		}
		if cli.snatch {
			if err := varroa.SnatchTorrents(env, tracker, intslice.ToStringSlice(cli.torrentIDs), cli.useFLToken); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorSnatchingTorrent), logthis.NORMAL)
			}
		}
		if cli.info {
			if err := varroa.ShowTorrentInfo(env, tracker, intslice.ToStringSlice(cli.torrentIDs)); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorShowingTorrentInfo), logthis.NORMAL)
			}
		}
		if cli.checkLog {
			if err := varroa.CheckLog(tracker, []string{cli.logFile}); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorCheckingLog), logthis.NORMAL)
			}
		}
		if cli.reseed {
			if err := varroa.Reseed(tracker, cli.paths); err != nil {
				logthis.Error(errors.Wrap(err, varroa.ErrorReseed), logthis.NORMAL)
			}
		}
	} else {
		// daemon is up, sending commands to the daemon through the unix socket
		if err := varroa.SendOrders(cli.commandToDaemon()); err != nil {
			logthis.Error(errors.Wrap(err, varroa.ErrorSendingCommandToDaemon), logthis.NORMAL)
			return
		}
		// at last, sending signals for shutdown
		if cli.stop {
			d.Stop(daemonProcess)
			return
		}
	}
}

func closeDB() {
	// closing statsDB properly
	if stats, err := varroa.NewDatabase(filepath.Join(varroa.StatsDir, varroa.DefaultHistoryDB)); err == nil {
		if closingErr := stats.Close(); closingErr != nil {
			logthis.Error(closingErr, logthis.NORMAL)
		}
	}
}
