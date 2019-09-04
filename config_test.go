package varroa

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/passelecasque/obstruction/tracker"
)

func TestConfig(t *testing.T) {
	fmt.Println("+ Testing Config...")
	check := assert.New(t)

	c := &Config{}
	err := c.Load("test/test_complete.yaml")
	check.Nil(err)

	// setting up
	check.Nil(os.Mkdir("library", 0777))
	defer os.Remove("library")

	// general
	check.Equal("test", c.General.WatchDir)
	check.Equal("../varroa/test", c.General.DownloadDir)
	check.Equal(2, c.General.LogLevel)
	check.True(c.General.AutomaticMetadataRetrieval)

	// trackers
	fmt.Println("Checking trackers")
	check.Equal(2, len(c.Trackers))
	tr := c.Trackers[0]
	check.Equal("blue", tr.Name)
	check.Equal("username", tr.User)
	check.Equal("secretpassword", tr.Password)
	check.Equal("", tr.Cookie)
	check.Equal("https://blue.ch", tr.URL)
	tr = c.Trackers[1]
	check.Equal("purple", tr.Name)
	check.Equal("another_username", tr.User)
	check.Equal("", tr.Password)
	check.Equal("cookievalue", tr.Cookie)
	check.Equal("https://purple.cd", tr.URL)
	// autosnatch
	fmt.Println("Checking autosnatch")
	check.Equal(2, len(c.Autosnatch))
	a := c.Autosnatch[0]
	check.Equal("blue", a.Tracker)
	check.Equal("1.2.3.4", a.LocalAddress)
	check.Equal("irc.server.net:6697", a.IRCServer)
	check.Equal("kkeeyy", a.IRCKey)
	check.True(a.IRCSSL)
	check.False(a.IRCSSLSkipVerify)
	check.Equal("something", a.NickservPassword)
	check.Equal("mybot", a.BotName)
	check.Equal("Bee", a.Announcer)
	check.Equal("#blue-announce", a.AnnounceChannel)
	check.Equal([]string{"AwfulUser"}, a.BlacklistedUploaders)
	a = c.Autosnatch[1]
	check.Equal("purple", a.Tracker)
	check.Equal("irc.server.cd:6697", a.IRCServer)
	check.Equal("kkeeyy!", a.IRCKey)
	check.True(a.IRCSSL)
	check.True(a.IRCSSLSkipVerify)
	check.Equal("somethingŁ", a.NickservPassword)
	check.Equal("bobot", a.BotName)
	check.Equal("bolivar", a.Announcer)
	check.Equal("#announce", a.AnnounceChannel)
	check.Nil(a.BlacklistedUploaders)
	// stats
	fmt.Println("Checking stats")
	check.Equal(2, len(c.Stats))
	s := c.Stats[0]
	check.Equal("blue", s.Tracker)
	check.Equal(1, s.UpdatePeriodH)
	check.Equal(500, s.MaxBufferDecreaseMB)
	check.Equal(0.78, s.MinimumRatio)
	check.Equal(0.8, s.TargetRatio)
	s = c.Stats[1]
	check.Equal("purple", s.Tracker)
	check.Equal(12, s.UpdatePeriodH)
	check.Equal(2500, s.MaxBufferDecreaseMB)
	check.Equal(0.60, s.MinimumRatio)
	check.Equal(1.0, s.TargetRatio)
	// webserver
	fmt.Println("Checking webserver")
	check.True(c.WebServer.ServeStats)
	check.Equal(darkGreen, c.WebServer.Theme)
	check.True(c.WebServer.AllowDownloads)
	check.True(c.WebServer.ServeMetadata)
	check.Equal("httppassword", c.WebServer.Password)
	check.Equal("httpuser", c.WebServer.User)
	check.Equal("thisisatoken", c.WebServer.Token)
	check.Equal("server.that.is.mine.com", c.WebServer.Hostname)
	check.Equal(1234, c.WebServer.PortHTTP)
	check.Equal(1235, c.WebServer.PortHTTPS)
	// pushover notifications
	fmt.Println("Checking pushover notifications")
	check.Equal("tokenpushovertoken", c.Notifications.Pushover.Token)
	check.Equal("userpushoveruser", c.Notifications.Pushover.User)
	check.Equal(true, c.Notifications.Pushover.IncludeBufferGraph)
	// irc notifications
	fmt.Println("Checking IRC notifications")
	check.Equal("blue", c.Notifications.Irc.Tracker)
	check.Equal("irc_name", c.Notifications.Irc.User)
	// webhooks
	fmt.Println("Checking webhooks")
	check.Equal("http://some.thing", c.Notifications.WebHooks.Address)
	check.Equal("tokenwebhooktoken", c.Notifications.WebHooks.Token)
	check.Equal([]string{"blue"}, c.Notifications.WebHooks.Trackers)
	// library
	fmt.Println("Checking library")
	check.Equal("test", c.Library.Directory)
	check.True(c.Library.UseHardLinks)
	check.Equal("$a/$a ($y) $t [$f $q] [$s] [$l $n $e]", c.Library.Template)
	check.Equal([]string{"../varroa/test", "../varroa/cmd"}, c.Library.AdditionalSources)
	check.Equal("test/aliases.yaml", c.Library.AliasesFile)
	check.Equal(14, len(c.Library.Aliases["MF DOOM"]))
	check.Equal([]string{"Bergman Rock"}, c.Library.Aliases["bob hund"])
	check.Equal("test/categories.yaml", c.Library.CategoriesFile)
	check.Equal(2, len(c.Library.Categories))
	check.Equal([]string{"Skip James", "VA| All the Blues: All of it"}, c.Library.Categories["Blues"])
	check.Equal([]string{"The Black Keys", "The Jon Spencer Blues Explosion"}, c.Library.Categories["Blues-Rock"])
	check.Equal("test", c.Library.PlaylistDirectory)

	// gitlab
	fmt.Println("Checking gitlab pages")
	check.Equal("https://gitlab.com/something/repo.git", c.GitlabPages.GitHTTPS)
	check.Equal("gitlabuser", c.GitlabPages.User)
	check.Equal("anotherpassword", c.GitlabPages.Password)
	check.Equal("https://something.gitlab.io/repo", c.GitlabPages.URL)
	// mpd
	fmt.Println("Checking mpd")
	check.Equal("localhost:1234", c.MPD.Server)
	check.Equal("optional", c.MPD.Password)
	check.Equal("../varroa/test", c.MPD.Library)
	// metadata
	fmt.Println("Checking metadata")
	check.Equal("THISISASECRETTOKENGENERATEDFROMDISCOGSACCOUNT", c.Metadata.DiscogsToken)
	// filters
	fmt.Println("Checking filters")
	check.Equal(2, len(c.Filters))
	fmt.Println("Checking filter 'perfect'")
	f := c.Filters[0]
	check.Equal("perfect", f.Name)
	check.Nil(f.Year)
	check.Equal(tracker.KnownSources, f.Source)
	check.Equal([]string{"FLAC"}, f.Format)
	check.Equal([]string{"24bit Lossless", "Lossless"}, f.Quality)
	check.True(f.HasCue)
	check.True(f.HasLog)
	check.Equal(100, f.LogScore)
	check.False(f.AllowScene)
	check.False(f.AllowDuplicates)
	check.Nil(f.ReleaseType)
	check.Equal([]string{"Concert Recording"}, f.ExcludedReleaseType)
	check.Equal("", f.WatchDir)
	check.Equal(0, f.MinSizeMB)
	check.Equal(0, f.MaxSizeMB)
	check.Nil(f.TagsRequired)
	check.Nil(f.TagsIncluded)
	check.Nil(f.TagsExcluded)
	check.Nil(f.Artist)
	check.Nil(f.RecordLabel)
	check.True(f.PerfectFlac)
	check.True(f.UniqueInGroup)
	check.Equal([]string{"blue"}, f.Tracker)
	check.Equal([]string{"best_uploader_ever", "this other guy"}, f.Uploader)
	check.True(f.RejectUnknown)
	check.Equal([]string{"Bonus", "Anniversary", "r/[dD]eluxe", "xr/[cC][lL][eE][aA][nN]"}, f.Edition)
	check.Equal([]int{2014, 2015}, f.EditionYear)
	check.Equal([]string{"ThisOtherGuy"}, f.BlacklistedUploader)
	fmt.Println("Checking filter 'test'")
	f = c.Filters[1]
	check.Equal("test", f.Name)
	check.Equal([]int{2016, 2017}, f.Year)
	check.Equal([]string{"CD", "WEB"}, f.Source)
	check.Equal([]string{"FLAC", "MP3"}, f.Format)
	check.Equal([]string{"Lossless", "24bit Lossless", "320", "V0 (VBR)"}, f.Quality)
	check.True(f.HasCue)
	check.True(f.HasLog)
	check.Equal(80, f.LogScore)
	check.True(f.AllowScene)
	check.True(f.AllowDuplicates)
	check.Equal([]string{"Album", "EP"}, f.ReleaseType)
	check.Nil(f.ExcludedReleaseType)
	check.Equal("test", f.WatchDir)
	check.Equal(10, f.MinSizeMB)
	check.Equal(500, f.MaxSizeMB)
	check.Equal([]string{"indie"}, f.TagsRequired)
	check.Equal([]string{"hip.hop", "pop"}, f.TagsIncluded)
	check.Equal([]string{"metal"}, f.TagsExcluded)
	check.Equal([]string{"The Beatles"}, f.Artist)
	check.Equal([]string{"Warp"}, f.RecordLabel)
	check.False(f.PerfectFlac)
	check.False(f.UniqueInGroup)
	check.Nil(f.Tracker)
	check.Nil(f.Uploader)
	check.False(f.RejectUnknown)
	check.Nil(f.Edition)
	check.Nil(f.EditionYear)

	check.True(c.autosnatchConfigured)
	check.True(c.statsConfigured)
	check.True(c.webserverConfigured)
	check.True(c.gitlabPagesConfigured)
	check.True(c.pushoverConfigured)
	check.True(c.webhooksConfigured)
	check.True(c.DownloadFolderConfigured)
	check.True(c.webserverHTTP)
	check.True(c.webserverHTTPS)
	check.True(c.LibraryConfigured)
	check.True(c.playlistDirectoryConfigured)
	check.True(c.metadataConfigured)
	check.True(c.discogsTokenConfigured)

	// disabling autosnatch
	check.False(c.Autosnatch[0].disabledAutosnatching)
	autosnatchConfig, err := c.GetAutosnatch("blue")
	check.Nil(err)
	autosnatchConfig.disabledAutosnatching = true
	check.True(c.Autosnatch[0].disabledAutosnatching)
	// enabling again
	for _, a := range c.Autosnatch {
		a.disabledAutosnatching = false
	}
	check.False(c.Autosnatch[0].disabledAutosnatching)

	// quick testing of files that only use a few features
	c = &Config{}
	err = c.Load("test/test_nostats.yaml")
	check.Nil(err)
	check.True(c.autosnatchConfigured)
	check.False(c.statsConfigured)
	check.True(c.webserverConfigured)
	check.False(c.gitlabPagesConfigured)
	check.False(c.pushoverConfigured)
	check.True(c.DownloadFolderConfigured)
	check.False(c.webserverHTTP)
	check.True(c.webserverHTTPS)
	check.False(c.LibraryConfigured)

	c = &Config{}
	err = c.Load("test/test_nostatsnoweb.yaml")
	check.Nil(err)
	check.True(c.autosnatchConfigured)
	check.False(c.statsConfigured)
	check.False(c.webserverConfigured)
	check.False(c.gitlabPagesConfigured)
	check.False(c.pushoverConfigured)
	check.False(c.DownloadFolderConfigured)
	check.False(c.webserverHTTP)
	check.False(c.webserverHTTPS)
	check.False(c.LibraryConfigured)

	c = &Config{}
	err = c.Load("test/test_statsnoautosnatch.yaml")
	check.Nil(err)
	check.False(c.autosnatchConfigured)
	check.True(c.statsConfigured)
	check.True(c.webserverConfigured)
	check.True(c.gitlabPagesConfigured)
	check.True(c.pushoverConfigured)
	check.True(c.DownloadFolderConfigured)
	check.True(c.webserverHTTP)
	check.True(c.webserverHTTPS)
	check.False(c.LibraryConfigured)

}
