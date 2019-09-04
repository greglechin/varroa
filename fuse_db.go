package varroa

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/briandowns/spinner"
	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/catastrophic/assistance/strslice"
	"gitlab.com/passelecasque/obstruction/tracker"
)

// FuseEntry is the struct describing a release folder with tracker metadata.
// Only the FolderName is indexed.
type FuseEntry struct {
	ID          int      `storm:"id,increment"`
	FolderName  string   `storm:"unique"`
	Artists     []string `storm:"index"`
	Tags        []string `storm:"index"`
	Title       string   `storm:"index"`
	Year        int      `storm:"index"`
	Tracker     []string `storm:"index"`
	RecordLabel string   `storm:"index"`
	Source      string   `storm:"index"`
	Format      string   `storm:"index"`
}

func (fe *FuseEntry) reset() {
	fe.Artists = []string{}
	fe.Tags = []string{}
	fe.Title = ""
	fe.Year = 0
	fe.Tracker = []string{}
	fe.RecordLabel = ""
	fe.Source = ""
	fe.Format = ""
}

func (fe *FuseEntry) Load(root string) error {
	if fe.FolderName == "" || !fs.DirExists(filepath.Join(root, fe.FolderName)) {
		return errors.New("Wrong or missing path")
	}

	// find origin.json
	originFile := filepath.Join(root, fe.FolderName, MetadataDir, OriginJSONFile)
	if fs.FileExists(originFile) {
		origin := TrackerOriginJSON{Path: originFile}
		if err := origin.Load(); err != nil {
			return errors.Wrap(err, "Error reading origin.json")
		}
		// reset fields
		fe.reset()

		// TODO: remove duplicate if there are actually several origins

		// load useful things from JSON
		for trackerName := range origin.Origins {
			fe.Tracker = append(fe.Tracker, trackerName)

			// getting release info from json
			infoJSON := filepath.Join(root, fe.FolderName, MetadataDir, trackerName+"_"+trackerMetadataFile)
			if !fs.FileExists(infoJSON) {
				// if not present, try the old format
				infoJSON = filepath.Join(root, fe.FolderName, MetadataDir, "Release.json")
			}
			if fs.FileExists(infoJSON) {
				// load JSON, get info
				md := TrackerMetadata{}
				if err := md.LoadFromJSON(trackerName, originFile, infoJSON); err != nil {
					return errors.Wrap(err, "Error loading JSON file "+infoJSON)
				}
				// extract relevant information!
				// for now, using artists, composers, "with" categories
				// extract relevant information!
				for _, a := range md.Artists {
					fe.Artists = append(fe.Artists, a.Name)
				}
				fe.RecordLabel = fs.SanitizePath(md.RecordLabel)
				fe.Year = md.OriginalYear // only show original year
				fe.Title = md.Title
				fe.Tags = md.Tags
				fe.Source = md.SourceFull
				fe.Format = tracker.ShortEncoding(md.Quality)
			}
		}
	} else {
		return errors.New("Error, no metadata found")
	}
	return nil
}

type FuseDB struct {
	Database
	Root string
}

func (fdb *FuseDB) Scan(rootPath string) error {
	defer TimeTrack(time.Now(), "Scan FuseDB")

	if fdb.DB == nil {
		return errors.New("Error db not open")
	}
	if err := fdb.DB.Init(&FuseEntry{}); err != nil {
		return errors.New("Could not prepare database for indexing fuse entries")
	}

	if !fs.DirExists(rootPath) {
		return errors.New("Error finding " + rootPath)
	}
	fdb.Root = rootPath

	s := spinner.New([]string{"    ", ".   ", "..  ", "... "}, 150*time.Millisecond)
	s.Prefix = scanningFiles
	s.Start()

	// get old entries
	var previous []FuseEntry
	if err := fdb.DB.All(&previous); err != nil {
		return errors.New("Cannot load previous entries")
	}

	tx, err := fdb.DB.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentFolderNames []string

	walkErr := filepath.Walk(fdb.Root, func(path string, fileInfo os.FileInfo, walkError error) error {
		// when an album has just been moved, Walk goes through it a second
		// time with an "file does not exist" error
		if os.IsNotExist(walkError) {
			return nil
		}
		// if it is the top directory of a release with metadata
		if fileInfo.IsDir() && DirectoryContainsMusicAndMetadata(path) {
			// try to find entry
			var fuseEntry FuseEntry

			relativeFolderName, err := filepath.Rel(rootPath, path)
			if err != nil {
				return err
			}
			if dbErr := fdb.DB.One("FolderName", relativeFolderName, &fuseEntry); dbErr != nil {
				if dbErr == storm.ErrNotFound {
					// not found, create new entry
					fuseEntry.FolderName = relativeFolderName
					// read information from metadata
					if err := fuseEntry.Load(fdb.Root); err != nil {
						logthis.Error(errors.Wrap(err, "Error: could not load metadata for "+relativeFolderName), logthis.VERBOSEST)
						return err
					}
					if err := tx.Save(&fuseEntry); err != nil {
						logthis.Info("Error: could not save to db "+relativeFolderName, logthis.VERBOSEST)
						return err
					}
					logthis.Info("New FuseDB entry: "+relativeFolderName, logthis.VERBOSESTEST)
				} else {
					logthis.Error(dbErr, logthis.VERBOSEST)
					return dbErr
				}
			} else {
				// found entry, update it
				// TODO for existing entries, maybe only reload if the metadata has been modified?
				// read information from metadata
				if err := fuseEntry.Load(fdb.Root); err != nil {
					logthis.Info("Error: could not load metadata for "+relativeFolderName, logthis.VERBOSEST)
					return err
				}
				if err := tx.Update(&fuseEntry); err != nil {
					logthis.Info("Error: could not save to db "+relativeFolderName, logthis.VERBOSEST)
					return err
				}
				logthis.Info("Updated FuseDB entry: "+relativeFolderName, logthis.VERBOSESTEST)
			}
			currentFolderNames = append(currentFolderNames, relativeFolderName)
		}
		return nil
	})
	if walkErr != nil {
		logthis.Error(walkErr, logthis.NORMAL)
	}

	// remove entries no longer associated with actual files
	for _, p := range previous {
		if !strslice.Contains(currentFolderNames, p.FolderName) {
			if err := tx.DeleteStruct(&p); err != nil {
				logthis.Error(err, logthis.VERBOSEST)
			}
			logthis.Info("Removed FuseDB entry: "+p.FolderName, logthis.VERBOSESTEST)
		}
	}

	defer TimeTrack(time.Now(), "Committing changes to DB")
	if err := tx.Commit(); err != nil {
		return err
	}

	s.Stop()
	return nil
}

func (fdb *FuseDB) contains(category, value string, inSlice bool) bool {
	var query storm.Query
	if inSlice {
		query = fdb.DB.Select(InSlice(category, value)).Limit(1)
	} else {
		query = fdb.DB.Select(q.Eq(category, value)).Limit(1)
	}
	var entry FuseEntry
	if err := query.First(&entry); err != nil {
		if err == storm.ErrNotFound {
			logthis.Info("Unknown value for "+category+": "+value, logthis.VERBOSEST)
			return false
		}
		logthis.Error(err, logthis.VERBOSEST)
		return false
	}
	return true
}

func (fdb *FuseDB) uniqueEntries(matcher q.Matcher, field string) ([]string, error) {
	// get all matching entries
	var allEntries []FuseEntry
	query := fdb.DB.Select(matcher)
	if err := query.Find(&allEntries); err != nil {
		logthis.Error(err, logthis.VERBOSEST)
		return []string{}, err
	}
	// get all different values
	var allValues []string
	for _, e := range allEntries {
		switch field {
		case "Tags":
			allValues = append(allValues, e.Tags...)
		case "Source":
			allValues = append(allValues, e.Source)
		case "Format":
			allValues = append(allValues, e.Format)
		case "Year":
			allValues = append(allValues, strconv.Itoa(e.Year))
		case "RecordLabel":
			allValues = append(allValues, e.RecordLabel)
		case "Artists":
			allValues = append(allValues, e.Artists...)
		case "FolderName":
			allValues = append(allValues, e.FolderName)
		}
	}
	strslice.RemoveDuplicates(&allValues)
	return allValues, nil
}
