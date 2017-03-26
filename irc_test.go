package main

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	filter1  = &Filter{label: "filter1", artist: []string{"another one"}}
	filter2  = &Filter{label: "filter2", artist: []string{"Another one"}}
	filter3  = &Filter{label: "filter3", artist: []string{"Aníkúlápó"}}
	filter4  = &Filter{label: "filter4", artist: []string{"An artist"}}
	filter5  = &Filter{label: "filter5", artist: []string{"An artist"}, format: []string{"FLAC"}}
	filter6  = &Filter{label: "filter6", artist: []string{"An artist"}, format: []string{"FLAC", "MP3"}}
	filter7  = &Filter{label: "filter7", format: []string{"AAC"}}
	filter8  = &Filter{label: "filter8", source: []string{"CD"}, hasLog: true}
	filter9  = &Filter{label: "filter9", year: []int{1999}}
	filter10 = &Filter{label: "filter10", year: []int{1999}, allowScene: true}
	filter11 = &Filter{label: "filter11", artist: []string{"Another !ONE", "his friend"}}
	filter12 = &Filter{label: "filter12", releaseType: []string{"Album", "Anthology"}}
	filter13 = &Filter{label: "filter13", quality: []string{"Lossless", "24bit Lossless"}, allowScene: true}
	filter14 = &Filter{label: "filter14", source: []string{"Vinyl", "Cassette"}, allowScene: true}
	filter15 = &Filter{label: "filter15", source: []string{"CD"}, hasLog: true, logScore: 100}
	filter16 = &Filter{label: "filter16", source: []string{"CD"}, logScore: 80} // will trigger even if hasLog == false

	allFilters = []*Filter{filter1, filter2, filter3, filter4, filter5, filter6, filter7, filter8, filter9, filter10, filter11, filter12, filter13, filter14, filter15, filter16}
)

var announces = []struct {
	announce         string
	expectedHit      bool
	expectedRelease  string
	satisfiedFilters []*Filter
}{
	{
		`An artist - Title / \ with utf8 characters éç_?<Ω>§Ð¢<¢<Ð> [2013] [Album] - MP3 / 320 / CD - https://mysterious.address/torrents.php?id=93821 / https://mysterious.address/torrents.php?action=download&id=981243 - tag1.taggy,tag2.mctagface`,
		true,
		"Release info:\n\tArtist: An artist\n\tTitle: Title / \\ with utf8 characters éç_?<Ω>§Ð¢<¢<Ð>\n\tYear: 2013\n\tRelease Type: Album\n\tFormat: MP3\n\tQuality: 320\n\tHasLog: false\n\tLog Score: -9999\n\tHas Cue: false\n\tScene: false\n\tSource: CD\n\tTags: [tag1.taggy tag2.mctagface]\n\tURL: https://mysterious.address/torrents.php?id=93821\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=981243\n\tTorrent ID: 981243",
		[]*Filter{filter4, filter6, filter12, filter16},
	},
	{
		`An artist:!, with / another artist! :)ÆΩ¢ - Title / \ with - utf8 characters éç_?<Ω>§Ð¢<¢<Ð> [1999] [EP] - FLAC / 24bit Lossless / Vinyl / Scene - https://mysterious.address/torrents.php?id=93821 / https://mysterious.address/torrents.php?action=download&id=981243 - tag.mctagface`,
		true,
		"Release info:\n\tArtist: An artist:!, with / another artist! :)ÆΩ¢\n\tTitle: Title / \\ with - utf8 characters éç_?<Ω>§Ð¢<¢<Ð>\n\tYear: 1999\n\tRelease Type: EP\n\tFormat: FLAC\n\tQuality: 24bit Lossless\n\tHasLog: false\n\tLog Score: -9999\n\tHas Cue: false\n\tScene: true\n\tSource: Vinyl\n\tTags: [tag.mctagface]\n\tURL: https://mysterious.address/torrents.php?id=93821\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=981243\n\tTorrent ID: 981243",
		[]*Filter{filter10, filter13, filter14},
	},
	{
		`A - B - X [1999] [Live album] - AAC / 256 / WEB - https://mysterious.address/torrents.php?id=93821 / https://mysterious.address/torrents.php?action=download&id=981243 - tag.mctagface`,
		true,
		"Release info:\n\tArtist: A\n\tTitle: B - X\n\tYear: 1999\n\tRelease Type: Live album\n\tFormat: AAC\n\tQuality: 256\n\tHasLog: false\n\tLog Score: -9999\n\tHas Cue: false\n\tScene: false\n\tSource: WEB\n\tTags: [tag.mctagface]\n\tURL: https://mysterious.address/torrents.php?id=93821\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=981243\n\tTorrent ID: 981243",
		[]*Filter{filter7, filter9, filter10},
	},
	{
		"First dude & another one performed by yet another & his friend - Title [1992] [Soundtrack] - FLAC / Lossless / Cassette - https://mysterious.address/torrents.php?id=452658 / https://mysterious.address/torrents.php?action=download&id=922578 - classical",
		true,
		"Release info:\n\tArtist: First dude & another one performed by yet another & his friend,First dude,another one,yet another,his friend\n\tTitle: Title\n\tYear: 1992\n\tRelease Type: Soundtrack\n\tFormat: FLAC\n\tQuality: Lossless\n\tHasLog: false\n\tLog Score: -9999\n\tHas Cue: false\n\tScene: false\n\tSource: Cassette\n\tTags: [classical]\n\tURL: https://mysterious.address/torrents.php?id=452658\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=922578\n\tTorrent ID: 922578",
		[]*Filter{filter1, filter11, filter13, filter14},
	},
	{
		"Various Artists - Something about Blues (Second Edition) [2016] [Compilation] - MP3 / V0 (VBR) / WEB - https://mysterious.address/torrents.php?id=452491 / https://mysterious.address/torrents.php?action=download&id=922592 - blues",
		true,
		"Release info:\n\tArtist: Various Artists\n\tTitle: Something about Blues (Second Edition)\n\tYear: 2016\n\tRelease Type: Compilation\n\tFormat: MP3\n\tQuality: V0 (VBR)\n\tHasLog: false\n\tLog Score: -9999\n\tHas Cue: false\n\tScene: false\n\tSource: WEB\n\tTags: [blues]\n\tURL: https://mysterious.address/torrents.php?id=452491\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=922592\n\tTorrent ID: 922592",
		[]*Filter{filter1, filter2, filter3, filter4, filter6, filter11},
	},
	{
		"Some fellow & Aníkúlápó - first / second [1999] [Anthology] - FLAC / Lossless / Log / Cue / CD - https://mysterious.address/torrents.php?id=271487 / https://mysterious.address/torrents.php?action=download&id=923266 - soul, funk, afrobeat, world.music",
		true,
		"Release info:\n\tArtist: Some fellow & Aníkúlápó,Some fellow,Aníkúlápó\n\tTitle: first / second\n\tYear: 1999\n\tRelease Type: Anthology\n\tFormat: FLAC\n\tQuality: Lossless\n\tHasLog: true\n\tLog Score: -9999\n\tHas Cue: true\n\tScene: false\n\tSource: CD\n\tTags: [soul funk afrobeat world.music]\n\tURL: https://mysterious.address/torrents.php?id=271487\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=923266\n\tTorrent ID: 923266",
		[]*Filter{filter3, filter8, filter9, filter10, filter12, filter13, filter15, filter16},
	},
	{
		"Non-music artist - Ebook Title!  - https://mysterious.address/torrents.php?id=452618 / https://mysterious.address/torrents.php?action=download&id=922495 - science.fiction,medieval.history",
		false,
		"",
		[]*Filter{},
	},
	{
		"Some fellow & Aníkúlápó - first / second [1999] [Anthology] - FLAC / Lossless / Log / 100% / Cue / CD - https://mysterious.address/torrents.php?id=271487 / https://mysterious.address/torrents.php?action=download&id=923266 - soul, funk, afrobeat, world.music",
		true,
		"Release info:\n\tArtist: Some fellow & Aníkúlápó,Some fellow,Aníkúlápó\n\tTitle: first / second\n\tYear: 1999\n\tRelease Type: Anthology\n\tFormat: FLAC\n\tQuality: Lossless\n\tHasLog: true\n\tLog Score: 100\n\tHas Cue: true\n\tScene: false\n\tSource: CD\n\tTags: [soul funk afrobeat world.music]\n\tURL: https://mysterious.address/torrents.php?id=271487\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=923266\n\tTorrent ID: 923266",
		[]*Filter{filter3, filter8, filter9, filter10, filter12, filter13, filter15, filter16},
	},
	{
		"Some fellow & Aníkúlápó - first / second [1999] [Anthology] - FLAC / Lossless / Log / 95% / Cue / CD - https://mysterious.address/torrents.php?id=271487 / https://mysterious.address/torrents.php?action=download&id=923266 - soul, funk, afrobeat, world.music",
		true,
		"Release info:\n\tArtist: Some fellow & Aníkúlápó,Some fellow,Aníkúlápó\n\tTitle: first / second\n\tYear: 1999\n\tRelease Type: Anthology\n\tFormat: FLAC\n\tQuality: Lossless\n\tHasLog: true\n\tLog Score: 95\n\tHas Cue: true\n\tScene: false\n\tSource: CD\n\tTags: [soul funk afrobeat world.music]\n\tURL: https://mysterious.address/torrents.php?id=271487\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=923266\n\tTorrent ID: 923266",
		[]*Filter{filter3, filter8, filter9, filter10, filter12, filter13, filter16},
	},
	{
		"Some fellow & Aníkúlápó - first / second [1999] [Anthology] - FLAC / Lossless / Log / -75% / Cue / CD - https://mysterious.address/torrents.php?id=271487 / https://mysterious.address/torrents.php?action=download&id=923266 - soul, funk, afrobeat, world.music",
		true,
		"Release info:\n\tArtist: Some fellow & Aníkúlápó,Some fellow,Aníkúlápó\n\tTitle: first / second\n\tYear: 1999\n\tRelease Type: Anthology\n\tFormat: FLAC\n\tQuality: Lossless\n\tHasLog: true\n\tLog Score: -75\n\tHas Cue: true\n\tScene: false\n\tSource: CD\n\tTags: [soul funk afrobeat world.music]\n\tURL: https://mysterious.address/torrents.php?id=271487\n\tTorrent URL: https://mysterious.address/torrents.php?action=download&id=923266\n\tTorrent ID: 923266",
		[]*Filter{filter3, filter8, filter9, filter10, filter12, filter13},
	},
}

func TestRegexp(t *testing.T) {
	fmt.Println("+ Testing Announce parsing & filtering...")
	verify := assert.New(t)

	for _, announced := range announces {
		r := regexp.MustCompile(announcePattern)
		hits := r.FindAllStringSubmatch(announced.announce, -1)
		if announced.expectedHit {
			verify.NotZero(len(hits))
			release, err := NewRelease(hits[0])
			verify.Nil(err)
			verify.Equal(announced.expectedRelease, release.String())
			fmt.Println(release)
			satisfied := 0
			for _, f := range allFilters {
				if release.Satisfies(*f) {
					found := false
					for _, ef := range announced.satisfiedFilters {
						if f == ef {
							found = true
							fmt.Println("=> triggers " + f.label + " (expected)")
							break
						}
					}
					if !found {
						fmt.Println("=> triggers " + f.label + " (UNexpected!)")
					}
					verify.True(found)
					satisfied++
				}
			}
			verify.Equal(len(announced.satisfiedFilters), satisfied)

		} else {
			verify.Zero(len(hits))
		}

	}

}
