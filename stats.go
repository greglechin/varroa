package varroa

import (
	"path/filepath"
	"reflect"
	"time"

	"github.com/asdine/storm"
	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/logthis"
)

func updateStats(e *Environment, tracker string, stats *StatsDB) error {
	// read configuration for this tracker
	statsConfig, err := e.config.GetStats(tracker)
	if err != nil {
		return errors.Wrap(err, "Error loading stats config for "+tracker)
	}

	// collect user stats
	gazelleTracker, err := e.Tracker(tracker)
	if err != nil {
		return errors.Wrap(err, "Error getting tracker info for "+tracker)
	}
	gzStats, err := gazelleTracker.GetLoggedUserStats()
	if err != nil {
		return errors.Wrap(err, errorGettingStats)
	}
	newStats, err := NewStatsEntry(gazelleTracker, gzStats)
	if err != nil {
		return errors.Wrap(err, errorGettingStats)
	}

	// save to database
	if saveErr := stats.Save(newStats); saveErr != nil {
		return errors.Wrap(saveErr, "Error saving stats to database")
	}

	// get previous stats
	var previousStats StatsEntry
	knownPreviousStats, err := stats.GetLastCollected(tracker, 2)
	if err != nil {
		if err != storm.ErrNotFound {
			previousStats = StatsEntry{Collected: true}
		} else {
			return errors.Wrap(err, "Error retreiving previous stats for tracker "+tracker)
		}
	} else if len(knownPreviousStats) == 2 {
		// we just save the first one, getting the penultimate
		previousStats = knownPreviousStats[1]
	}

	// compare with new stats
	logthis.Info(newStats.Progress(&previousStats), logthis.NORMAL)
	// send notification
	if notifyErr := Notify("stats: "+newStats.Progress(&previousStats), tracker, "info", e); notifyErr != nil {
		logthis.Error(notifyErr, logthis.NORMAL)
	}

	// if something is wrong, send notification and stop
	if !newStats.IsProgressAcceptable(&previousStats, statsConfig.MaxBufferDecreaseMB, statsConfig.MinimumRatio) {
		if newStats.Ratio <= statsConfig.MinimumRatio {
			// unacceptable because of low ratio
			logthis.Info(tracker+": "+errorBelowWarningRatio, logthis.NORMAL)
			// sending notification
			if err := Notify(tracker+": "+errorBelowWarningRatio, tracker, "error", e); err != nil {
				logthis.Error(err, logthis.NORMAL)
			}
		} else {
			// unacceptable because of ratio drop
			logthis.Info(tracker+": "+errorBufferDrop, logthis.NORMAL)
			// sending notification
			if err := Notify(tracker+": "+errorBufferDrop, tracker, "error", e); err != nil {
				logthis.Error(err, logthis.NORMAL)
			}
		}
		// stopping things
		autosnatchConfig, err := e.config.GetAutosnatch(tracker)
		if err != nil {
			logthis.Error(errors.Wrap(err, "Cannot find autosnatch configuration for tracker "+tracker), logthis.NORMAL)
		} else {
			e.mutex.Lock()
			autosnatchConfig.disabledAutosnatching = true
			e.mutex.Unlock()
		}
	}

	// generate graphs
	return stats.GenerateAllGraphsForTracker(tracker)
}

func monitorAllStats(e *Environment) {
	if !e.config.statsConfigured {
		return
	}
	// access to statsDB
	stats, err := NewStatsDB(filepath.Join(StatsDir, DefaultHistoryDB))
	if err != nil {
		logthis.Error(errors.Wrap(err, "Error, could not access the stats database"), logthis.NORMAL)
		return
	}

	// track all different periods
	tickers := map[int][]string{}
	for label, t := range e.Trackers {
		if statsConfig, err := e.config.GetStats(t.Name); err == nil {
			// initial stats
			if err := updateStats(e, label, stats); err != nil {
				logthis.Error(errors.Wrap(err, ErrorGeneratingGraphs), logthis.NORMAL)
			}
			// get update period
			tickers[statsConfig.UpdatePeriodH] = append(tickers[statsConfig.UpdatePeriodH], label)
		}
	}
	// generate index.html
	if err := e.GenerateIndex(); err != nil {
		logthis.Error(errors.Wrap(err, "Error generating index.html"), logthis.NORMAL)
	}
	// deploy
	if err := e.DeployToGitlabPages(); err != nil {
		logthis.Error(errors.Wrap(err, errorDeploying), logthis.NORMAL)
	}

	// preparing
	tickerChans := make([]<-chan time.Time, len(tickers))
	tickerPeriods := make([]int, len(tickers))
	cpt := 0
	for p := range tickers {
		tickerChans[cpt] = time.NewTicker(time.Hour * time.Duration(p)).C
		tickerPeriods[cpt] = p
		cpt++
	}
	cases := make([]reflect.SelectCase, len(tickerChans))
	for i, ch := range tickerChans {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}
	// wait for ticks
	for {
		triggered, _, ok := reflect.Select(cases)
		if !ok {
			// The triggered channel has been closed, so zero out the channel to disable the case
			cases[triggered].Chan = reflect.ValueOf(nil)
			continue
		}
		// TODO checks
		for _, trackerLabel := range tickers[tickerPeriods[triggered]] {
			if err := updateStats(e, trackerLabel, stats); err != nil {
				logthis.Error(errors.Wrap(err, ErrorGeneratingGraphs), logthis.NORMAL)
			}
		}
		// generate index.html
		if err := e.GenerateIndex(); err != nil {
			logthis.Error(errors.Wrap(err, "Error generating index.html"), logthis.NORMAL)
		}
		// deploy
		if err := e.DeployToGitlabPages(); err != nil {
			logthis.Error(errors.Wrap(err, errorDeploying), logthis.NORMAL)
		}
	}
}
