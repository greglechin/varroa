package varroa

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/passelecasque/obstruction/tracker"
)

func TestTrackerOriginJSON(t *testing.T) {
	fmt.Println("+ Testing TrackerOriginJSON...")
	check := assert.New(t)

	// setting up
	testDir := "test"
	env := &Environment{}

	c, err := NewConfig("test/test_complete.yaml")
	check.Nil(err)
	c.General.DownloadDir = testDir
	tr := &ConfigTracker{Name: "tracker1", URL: "http://azerty.com"}
	tr2 := &ConfigTracker{Name: "tracker2", URL: "http://qwerty.com"}
	c.Trackers = append(c.Trackers, tr, tr2)
	env.config = c
	tracker1, err := tracker.NewGazelle("tracker1", "http://azerty.com", "user", "password", "", "", userAgent())
	check.Nil(err)
	tracker2, err := tracker.NewGazelle("tracker2", "http://qwerty.com", "user", "password", "", "", userAgent())
	check.Nil(err)
	info1 := TrackerMetadata{ID: 1234, GroupID: 11, Tracker: tracker1.Name, TrackerURL: tracker1.DomainURL, LastUpdated: 1531651670, IsAlive: true}
	info2 := TrackerMetadata{ID: 1234, GroupID: 12, Tracker: tracker2.Name, TrackerURL: tracker2.DomainURL, LastUpdated: 1543701948}

	// make directory
	check.Nil(os.MkdirAll(filepath.Join(testDir, MetadataDir), 0775))
	defer os.Remove(filepath.Join(testDir, MetadataDir))
	expectedFilePath := filepath.Join(testDir, MetadataDir, OriginJSONFile)
	defer os.Remove(expectedFilePath)

	// saving origin JSON to file
	check.False(fs.FileExists(expectedFilePath))
	check.Nil(info1.saveOriginJSON(filepath.Join(testDir, MetadataDir)))
	check.True(fs.FileExists(expectedFilePath))
	check.Nil(info2.saveOriginJSON(filepath.Join(testDir, MetadataDir)))

	// reading file that was created and comparing with expected
	b, err := ioutil.ReadFile(expectedFilePath)
	check.Nil(err)
	var tojCheck TrackerOriginJSON
	check.Nil(json.Unmarshal(b, &tojCheck))
	check.Equal(info1.ID, tojCheck.Origins[tracker1.Name].ID)
	check.Equal(info1.GroupID, tojCheck.Origins[tracker1.Name].GroupID)
	check.Equal(info1.TrackerURL, tojCheck.Origins[tracker1.Name].Tracker)
	check.True(tojCheck.Origins[tracker1.Name].IsAlive)
	check.Equal(info1.TimeSnatched, tojCheck.Origins[tracker1.Name].TimeSnatched)
	check.Equal(info1.LastUpdated, tojCheck.Origins[tracker1.Name].LastUpdatedMetadata)

	check.Equal(info2.ID, tojCheck.Origins[tracker2.Name].ID)
	check.Equal(info2.GroupID, tojCheck.Origins[tracker2.Name].GroupID)
	check.Equal(info2.TrackerURL, tojCheck.Origins[tracker2.Name].Tracker)
	check.False(tojCheck.Origins[tracker2.Name].IsAlive)
	check.Equal(info2.TimeSnatched, tojCheck.Origins[tracker2.Name].TimeSnatched)
	check.Equal(info2.LastUpdated, tojCheck.Origins[tracker2.Name].LastUpdatedMetadata)

	lastUpdated := tojCheck.lastUpdated()
	check.Equal(2, len(lastUpdated))
	check.NotEqual(couldNotFindMetadataAge, tojCheck.lastUpdatedString())

	// update
	info1.LastUpdated = 2
	check.Nil(info1.saveOriginJSON(filepath.Join(testDir, MetadataDir)))

	// read from file again
	b, err = ioutil.ReadFile(expectedFilePath)
	check.Nil(err)
	check.Nil(json.Unmarshal(b, &tojCheck))
	check.Equal(info1.LastUpdated, tojCheck.Origins[tracker1.Name].LastUpdatedMetadata)
}
