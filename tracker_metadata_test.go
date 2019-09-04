package varroa

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/passelecasque/obstruction/tracker"
)

func TestGeneratePath(t *testing.T) {
	fmt.Println("+ Testing TrackerMetadata/generatePath...")
	check := assert.New(t)

	_, configErr := NewConfig("test/test_complete.yaml")
	check.Nil(configErr)

	// setup logger
	logthis.SetLevel(2)

	// test API JSON responses
	gt := tracker.GazelleTorrent{}
	gt.Response.Group.CatalogueNumber = "CATNUM Group"
	gt.Response.Group.MusicInfo.Artists = []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{
		{1,
			"Artist A",
		},
		{2,
			"Artist B",
		},
	}
	gt.Response.Group.Name = "RELEASE 1"
	gt.Response.Group.Year = 1987
	gt.Response.Group.RecordLabel = "LABEL 1 Group"
	gt.Response.Group.ReleaseType = 5 // EP
	gt.Response.Group.Tags = []string{"tag1", "tag2"}
	gt.Response.Group.WikiImage = "http://cover.jpg"
	gt.Response.Torrent.ID = 123
	gt.Response.Torrent.FilePath = "original_path"
	gt.Response.Torrent.Format = "FLAC"
	gt.Response.Torrent.Encoding = "Lossless"
	gt.Response.Torrent.Media = "WEB"
	gt.Response.Torrent.Remastered = true
	gt.Response.Torrent.RemasterCatalogueNumber = "CATNUM"
	gt.Response.Torrent.RemasterRecordLabel = "LABEL 1"
	gt.Response.Torrent.RemasterTitle = "Deluxe"
	gt.Response.Torrent.RemasterYear = 2017
	gt.Response.Torrent.HasLog = true
	gt.Response.Torrent.HasCue = true
	gt.Response.Torrent.LogScore = 100
	gt.Response.Torrent.FileList = "01 - First.flac{{{26538426}}}|||02 - Second.flac{{{32109249}}}"

	gt2 := gt
	gt2.Response.Torrent.Media = "CD"

	gt3 := gt2
	gt3.Response.Torrent.Format = "MP3"
	gt3.Response.Torrent.Encoding = "V0 (VBR)"
	gt3.Response.Torrent.RemasterTitle = "Bonus Tracks"

	gt4 := gt3
	gt4.Response.Torrent.Format = "FLAC"
	gt4.Response.Torrent.Encoding = "24bit Lossless"
	gt4.Response.Torrent.RemasterTitle = "Remaster"
	gt4.Response.Torrent.Media = "Vinyl"

	gt5 := gt4
	gt5.Response.Torrent.Grade = "Gold"
	gt5.Response.Torrent.Media = "CD"
	gt5.Response.Torrent.Encoding = "Lossless"

	gt6 := gt5
	gt6.Response.Torrent.Grade = "Silver"
	gt6.Response.Torrent.RemasterYear = 1987
	gt6.Response.Torrent.RemasterTitle = "Promo"
	gt6.Response.Group.ReleaseType = 1

	gt7 := gt6
	gt7.Response.Group.Name = "RELEASE 1 / RELEASE 2!!&éçà©§Ð‘®¢"

	gt8 := gt7
	gt8.Response.Group.Name = "\"Thing\""

	// tracker
	gzTracker, err := tracker.NewGazelle("BLUE", "http://blue", "user", "password", "", "", userAgent())
	check.Nil(err)

	// torrent infos
	infod2 := &TrackerMetadata{}
	check.Nil(infod2.Load(gzTracker, &gt))
	infod3 := &TrackerMetadata{}
	check.Nil(infod3.Load(gzTracker, &gt2))
	infod4 := &TrackerMetadata{}
	check.Nil(infod4.Load(gzTracker, &gt3))
	infod5 := &TrackerMetadata{}
	check.Nil(infod5.Load(gzTracker, &gt4))
	infod6 := &TrackerMetadata{}
	check.Nil(infod6.Load(gzTracker, &gt5))
	infod7 := &TrackerMetadata{}
	check.Nil(infod7.Load(gzTracker, &gt6))
	infod8 := &TrackerMetadata{}
	check.Nil(infod8.Load(gzTracker, &gt7))
	infod9 := &TrackerMetadata{}
	check.Nil(infod9.Load(gzTracker, &gt8))

	// checking GeneratePath
	check.Equal("original_path", infod2.GeneratePath("", ""))
	check.Equal("Artist A, Artist B", infod2.GeneratePath("$a", ""))
	check.Equal("RELEASE 1", infod2.GeneratePath("$t", ""))
	check.Equal("1987", infod2.GeneratePath("$y", ""))
	check.Equal("FLAC", infod2.GeneratePath("$f", ""))
	check.Equal("V0", infod4.GeneratePath("$f", ""))
	check.Equal("FLAC24", infod5.GeneratePath("$f", ""))
	check.Equal("WEB", infod2.GeneratePath("$s", ""))
	check.Equal("LABEL 1", infod2.GeneratePath("$l", ""))
	check.Equal("CATNUM", infod2.GeneratePath("$n", ""))
	check.Equal("DLX", infod2.GeneratePath("$e", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 [FLAC] [WEB]", infod2.GeneratePath("$a ($y) $t [$f] [$s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 [FLAC] [WEB] {DLX, LABEL 1-CATNUM}", infod2.GeneratePath("$a ($y) $t [$f] [$s] {$e, $l-$n}", ""))
	check.Equal("DLX/DLX", infod2.GeneratePath("$e/$e", "")) // sanitized to remove "/"
	check.Equal("2017, DLX, CATNUM", infod2.GeneratePath("$id", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, DLX, CATNUM} [FLAC WEB]", infod2.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, DLX, CATNUM} [FLAC CD]", infod3.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, DLX, CATNUM} [FLAC CD+]", infod3.GeneratePath("$a ($y) $t {$id} [$f $g]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, Bonus, CATNUM} [V0 CD]", infod4.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, RM, CATNUM} [FLAC24 Vinyl]", infod5.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, RM, CATNUM} [FLAC CD]", infod6.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, RM, CATNUM} [FLAC CD++]", infod6.GeneratePath("$a ($y) $t {$id} [$f $g]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {PR, CATNUM} [FLAC CD]", infod7.GeneratePath("$a ($y) $t {$id} [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {PR, CATNUM} [FLAC CD+]", infod7.GeneratePath("$a ($y) $t {$id} [$f $g]", ""))
	check.Equal("[Artist A, Artist B]/Artist A, Artist B (1987) RELEASE 1 {PR, CATNUM} [FLAC CD+]", infod7.GeneratePath("[$a]/$a ($y) $t {$id} [$f $g]", ""))
	check.Equal("[Artist A, Artist B]/Artist A, Artist B (1987) RELEASE 1 ∕ RELEASE 2!!&éçà©§Ð‘®¢ {PR, CATNUM} [FLAC CD+]", infod8.GeneratePath("[$a]/$a ($y) $t {$id} [$f $g]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, DLX, CATNUM} EP [FLAC WEB]", infod2.GeneratePath("$a ($y) $t {$id} $xar [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {2017, DLX, CATNUM} EP [FLAC WEB]", infod2.GeneratePath("$a ($y) $t {$id} $r [$f $s]", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {PR, CATNUM} [FLAC CD]", infod7.GeneratePath("$a ($y) $t {$id} [$f $s] $xar", ""))
	check.Equal("Artist A, Artist B (1987) RELEASE 1 {PR, CATNUM} [FLAC CD] Album", infod7.GeneratePath("$a ($y) $t {$id} [$f $s] $r", ""))
	check.Equal("Artist A, Artist B (1987) \"Thing\" {PR, CATNUM} [FLAC CD] Album", infod9.GeneratePath("$a ($y) $t {$id} [$f $s] $r", ""))

	// checking TextDescription
	fmt.Println(infod2.TextDescription(false))
	fmt.Println(infod2.TextDescription(true))
}

func TestArtistInSlice(t *testing.T) {
	fmt.Println("+ Testing TrackerMetadata/artistInSlice...")
	check := assert.New(t)

	list := []string{"thing", "VA| other thing", "VA|anoother thing", "VA |nope", "noope | VA"}
	check.True(artistInSlice("thing", "useless", list))
	check.False(artistInSlice("Thing", "useless", list))
	check.True(artistInSlice("Various Artists", "other thing", list))
	check.False(artistInSlice("Single Artist", "other thing", list))
	check.True(artistInSlice("Various Artists", "anoother thing", list))
	check.False(artistInSlice("Single Artist", "anoother thing", list))
	check.False(artistInSlice("Various Artists", "nope", list))
	check.False(artistInSlice("Single Artist", "nope", list))
	check.False(artistInSlice("Various Artists", "noope", list))
	check.False(artistInSlice("Various Artists", "VA | other thing", list))
	check.False(artistInSlice("Various Artists", "VA| other thing", list))
}
