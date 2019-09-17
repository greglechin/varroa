package varroa

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sevlyar/go-daemon"
	"github.com/wcharczuk/go-chart/drawing"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/git"
	"gitlab.com/catastrophic/assistance/ipc"
	"gitlab.com/catastrophic/assistance/logthis"
	irc "gitlab.com/catastrophic/go-ircevent"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	gitlabCI = `# plain-htlm CI
pages:
  stage: deploy
  script:
  - mkdir .public
  - cp -r * .public
  - mv .public public
  artifacts:
    paths:
    - public
  only:
  - master
`
)

// Environment keeps track of all the context varroa needs.
type Environment struct {
	config     *Config
	serverData *ServerPage
	Trackers   map[string]*tracker.Gazelle

	mutex            sync.RWMutex
	git              *git.Git
	daemonUnixSocket *ipc.UnixSocket
	startTime        time.Time
	ircClient        *irc.Connection
}

// NewEnvironment prepares a new Environment.
func NewEnvironment() *Environment {
	e := &Environment{}
	e.config = &Config{}
	e.serverData = &ServerPage{}
	// make maps
	e.Trackers = make(map[string]*tracker.Gazelle)
	e.daemonUnixSocket = ipc.NewUnixSocketServer(daemonSocket)
	// irc
	e.ircClient = nil
	return e
}

func (e *Environment) SetConfig(c *Config) {
	e.config = c
}

// LoadConfiguration whether the configuration file is encrypted or not.
func (e *Environment) LoadConfiguration() error {
	var err error
	e.config, err = NewConfig(DefaultConfigurationFile)
	if err != nil {
		return err
	}

	// get theme for stats & webserver
	if e.config.statsConfigured {
		theme := knownThemes[darkOrange]
		if e.config.webserverConfigured {
			theme = knownThemes[e.config.WebServer.Theme]
			commonStyleSVG.StrokeColor = drawing.ColorFromHex(theme.GraphColor[1:])
			commonStyleSVG.FillColor = drawing.ColorFromHex(theme.GraphColor[1:]).WithAlpha(theme.GraphFillerOpacity)
			commonStyleSVG.FontColor = drawing.ColorFromHex(theme.GraphAxisColor[1:])
			timeAxisSVG.NameStyle.FontColor = drawing.ColorFromHex(theme.GraphAxisColor[1:])
			timeAxisSVG.Style.FontColor = drawing.ColorFromHex(theme.GraphAxisColor[1:])
			timeAxisSVG.Style.StrokeColor = drawing.ColorFromHex(theme.GraphAxisColor[1:])
		}
		e.serverData.theme = theme
		e.serverData.index = HTMLIndex{Title: strings.ToUpper(FullName), Version: Version, CSS: theme.CSS(), Script: indexJS}
	}
	// git
	if e.config.gitlabPagesConfigured {
		e.git, err = git.New(StatsDir, e.config.GitlabPages.User, e.config.GitlabPages.User+"+varroa@musica")
		if err != nil {
			return err
		}
	}
	return nil
}

// SetUp the Environment
func (e *Environment) SetUp(autologin bool) error {
	// for uptime
	if daemon.WasReborn() {
		e.startTime = time.Now()
		// if in daemon, only use log file
		logthis.SetStdOutput(false)
	}
	// prepare directory for stats if necessary
	if !fs.DirExists(StatsDir) {
		if err := os.MkdirAll(StatsDir, 0777); err != nil {
			return errors.Wrap(err, errorCreatingStatsDir)
		}
	}
	// log in all trackers, assuming labels are unique (configuration was checked)
	for _, label := range e.config.TrackerLabels() {
		if _, err := e.setUpTracker(label, autologin); err != nil {
			return errors.Wrap(err, "Error setting up tracker "+label)
		}
	}
	return nil
}

func (e *Environment) setUpTracker(label string, autologin bool) (*tracker.Gazelle, error) {
	t, ok := e.Trackers[label]
	if !ok {
		// not found:
		trackerConfig, err := e.config.GetTracker(label)
		if err != nil {
			return nil, errors.Wrap(err, "Error getting tracker information")
		}
		t, err = tracker.NewGazelle(trackerConfig.Name, trackerConfig.URL, trackerConfig.User, trackerConfig.Password, "session", trackerConfig.Cookie, userAgent())
		if err != nil {
			return nil, errors.Wrap(err, "Error setting up tracker "+trackerConfig.Name)
		}
		// saving
		e.Trackers[label] = t
	}
	if t.Client == nil && autologin {
		if err := t.Login(); err != nil {
			return nil, errors.Wrap(err, "Error logging in tracker "+label)
		}
		logthis.Info(fmt.Sprintf("Logged in tracker %s.", label), logthis.NORMAL)
		// start rate limiter
		go t.RateLimiter()
	}
	return t, nil
}

func (e *Environment) Tracker(label string) (*tracker.Gazelle, error) {
	return e.setUpTracker(label, true)
}

func (e *Environment) GenerateIndex() error {
	if !e.config.statsConfigured {
		return nil
	}
	return e.serverData.SaveIndex(e, filepath.Join(StatsDir, htmlIndexFile))
}

// DeployToGitlabPages with git wrapper
func (e *Environment) DeployToGitlabPages() error {
	if !e.config.gitlabPagesConfigured {
		return nil
	}
	if e.git == nil {
		return errors.New("Error setting up git")
	}

	// init repository if necessary
	if !e.git.Exists() {
		if err := e.git.Init(); err != nil {
			return errors.Wrap(err, errorGitInit)
		}
		// create .gitlab-ci.yml
		if err := ioutil.WriteFile(filepath.Join(StatsDir, gitlabCIYamlFile), []byte(gitlabCI), 0666); err != nil {
			return err
		}
	}
	// add main files
	if err := e.git.Add(filepath.Base(gitlabCIYamlFile), filepath.Base(htmlIndexFile)); err != nil {
		return errors.Wrap(err, errorGitAdd)
	}
	// add the graphs, if it fails,
	if err := e.git.Add("*" + svgExt); err != nil {
		logthis.Error(errors.Wrap(err, errorGitAdd+", not all graphs are generated yet."), logthis.VERBOSEST)
	}
	// commit
	if err := e.git.Commit("varroa musica stats update."); err != nil {
		return errors.Wrap(err, errorGitCommit)
	}
	// push
	if !e.git.HasRemote("origin") {
		if err := e.git.AddRemote("origin", e.config.GitlabPages.GitHTTPS); err != nil {
			return errors.Wrap(err, errorGitAddRemote)
		}
	}
	if err := e.git.Push("origin", e.config.GitlabPages.GitHTTPS, e.config.GitlabPages.User, e.config.GitlabPages.Password); err != nil {
		return err
	}
	logthis.Info("Pushed new stats to "+e.config.GitlabPages.URL, logthis.NORMAL)
	return nil
}

func GoGoRoutines(e *Environment, noDaemon bool) {
	//  tracker-dependent goroutines
	for _, t := range e.Trackers {
		if e.config.autosnatchConfigured {
			go ircHandler(e, t)
		}
	}
	// general goroutines
	if e.config.statsConfigured {
		go monitorAllStats(e)
	}
	if e.config.webserverConfigured {
		go webServer(e)
	}
	// background goroutines
	go automatedTasks(e)
	if !noDaemon {
		go awaitOrders(e)
	}
}
