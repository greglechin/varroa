package varroa

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	progress      = "Buffer: %s (%s) | Ratio:  %.3f (%.3f) | Up: %s (%s) | Down: %s (%s) | Warning Buffer: %s (%s)"
	firstProgress = "Buffer: %s | Ratio: %.3f | Up: %s | Down: %s | Warning Buffer: %s"

	currentStatsDBSchemaVersion = 1
)

type StatsEntry struct {
	ID            uint32 `storm:"id,increment"`
	Tracker       string `storm:"index"`
	Up            uint64
	Down          uint64
	Ratio         float64
	Timestamp     time.Time
	TimestampUnix int64 `storm:"index"`
	Collected     bool  `storm:"index"`
	StartOfDay    bool  `storm:"index"`
	StartOfWeek   bool  `storm:"index"`
	StartOfMonth  bool  `storm:"index"`
	SchemaVersion int
}

func NewStatsEntry(gazelleTracker *tracker.Gazelle, gzStats *tracker.GazelleUserStats) (*StatsEntry, error) {
	ratio, err := strconv.ParseFloat(gzStats.Response.Stats.Ratio, 64)
	if err != nil {
		logthis.Info("Incorrect ratio: "+gzStats.Response.Stats.Ratio, logthis.NORMAL)
		ratio = 0.0
	}
	// return StatsEntry
	stats := &StatsEntry{
		Tracker:       gazelleTracker.Name,
		Up:            gzStats.Response.Stats.Uploaded,
		Down:          gzStats.Response.Stats.Downloaded,
		Ratio:         ratio,
		Timestamp:     time.Now(),
		TimestampUnix: time.Now().Unix(),
		Collected:     true,
		SchemaVersion: currentStatsDBSchemaVersion,
	}
	return stats, nil
}

func (se *StatsEntry) String() string {
	buffer, warningBuffer := se.getBufferValues()
	return fmt.Sprintf(firstProgress, fs.FileSizeDelta(buffer), se.Ratio, fs.FileSize(se.Up), fs.FileSize(se.Down), fs.FileSizeDelta(warningBuffer))
}

func (se *StatsEntry) getBufferValues() (int64, int64) {
	conf, err := NewConfig(DefaultConfigurationFile)
	if err != nil {
		logthis.Error(err, logthis.VERBOSEST)
		return 0, 0
	}
	statsConfig, err := conf.GetStats(se.Tracker)
	if err != nil {
		logthis.Error(err, logthis.VERBOSEST)
		return 0, 0
	}
	return int64(float64(se.Up)/statsConfig.TargetRatio) - int64(se.Down), int64(float64(se.Up)/warningRatio) - int64(se.Down)
}

// TODO REPLACE BY A DELTA
func (se *StatsEntry) Diff(previous *StatsEntry) (int64, int64, int64, int64, float64) {
	buffer, warningBuffer := se.getBufferValues()
	prevBuffer, prevWarningBuffer := previous.getBufferValues()
	return int64(se.Up - previous.Up), int64(se.Down - previous.Down), buffer - prevBuffer,
		warningBuffer - prevWarningBuffer, se.Ratio - previous.Ratio
}

func (se *StatsEntry) Progress(previous *StatsEntry) string {
	if previous.Ratio == 0 {
		return se.String()
	}
	buffer, warningBuffer := se.getBufferValues()
	dup, ddown, dbuff, dwbuff, dratio := se.Diff(previous)
	return fmt.Sprintf(progress, fs.FileSizeDelta(buffer), fs.FileSizeDelta(dbuff), se.Ratio, dratio, fs.FileSize(se.Up),
		fs.FileSizeDelta(dup), fs.FileSize(se.Down), fs.FileSizeDelta(ddown), fs.FileSizeDelta(warningBuffer),
		fs.FileSizeDelta(dwbuff))
}

// TODO do something about this awful thing
func (se *StatsEntry) ProgressParts(previous *StatsEntry) []string {
	buffer, warningBuffer := se.getBufferValues()
	if previous.Ratio == 0 {
		return []string{"+", se.Timestamp.Format("2006-01-02 15:04"), fs.FileSize(se.Up), fs.FileSize(se.Down), fs.FileSizeDelta(buffer), fs.FileSizeDelta(warningBuffer), fmt.Sprintf("%.3f", se.Ratio)}
	}
	dup, ddown, dbuff, dwbuff, dratio := se.Diff(previous)
	return []string{
		fs.Sign(dbuff),
		se.Timestamp.Format("2006-01-02 15:04"),
		fmt.Sprintf("%s (%s)", fs.FileSize(se.Up), fs.FileSizeDelta(dup)),
		fmt.Sprintf("%s (%s)", fs.FileSize(se.Down), fs.FileSizeDelta(ddown)),
		fmt.Sprintf("%s (%s)", fs.FileSizeDelta(buffer), fs.FileSizeDelta(dbuff)),
		fmt.Sprintf("%s (%s)", fs.FileSizeDelta(warningBuffer), fs.FileSizeDelta(dwbuff)),
		fmt.Sprintf("%.3f (%+.3f)", se.Ratio, dratio),
	}
}

func (se *StatsEntry) IsProgressAcceptable(previous *StatsEntry, maxDecrease int, minimumRatio float64) bool {
	if se.Ratio <= minimumRatio {
		logthis.Info("Ratio has dropped below minimum authorized, unacceptable.", logthis.NORMAL)
		return false
	}
	if previous.Ratio == 0 {
		// first pass
		return true
	}
	_, _, bufferChange, _, _ := se.Diff(previous)
	// if maxDecrease is unset (=0), always return true
	if maxDecrease == 0 || bufferChange >= 0 || -bufferChange <= int64(maxDecrease*1024*1024) {
		return true
	}
	logthis.Info(fmt.Sprintf("Decrease: %d bytes, only %d allowed. Unacceptable.", bufferChange, maxDecrease*1024*1024), logthis.VERBOSE)
	return false
}

// TODO reimplement export to CSV
func (se *StatsEntry) ToSlice() []string {
	// timestamp;up;down;ratio
	return []string{fmt.Sprintf("%d", se.Timestamp.Unix()), strconv.FormatUint(se.Up, 10), strconv.FormatUint(se.Down, 10), strconv.FormatFloat(se.Ratio, 'f', -1, 64)}
}

func InterpolateStats(previous, next StatsEntry, targetTime time.Time) (*StatsEntry, error) {
	// check targetTime is between se.Timest
	if targetTime.Before(previous.Timestamp) || targetTime.After(next.Timestamp) {
		return nil, errors.New("incorrect timestamp")
	}
	// create a virtual StatsEntry using simple linear interpolation
	virtualStats := &StatsEntry{}
	upSlope := (float64(next.Up) - float64(previous.Up)) / float64(next.Timestamp.Unix()-previous.Timestamp.Unix())
	upOffset := float64(previous.Up) - upSlope*float64(previous.Timestamp.Unix())
	virtualStats.Up = uint64(upSlope*float64(targetTime.Unix()) + upOffset)
	downSlope := (float64(next.Down) - float64(previous.Down)) / float64(next.Timestamp.Unix()-previous.Timestamp.Unix())
	downOffset := float64(previous.Down) - downSlope*float64(previous.Timestamp.Unix())
	virtualStats.Down = uint64(downSlope*float64(targetTime.Unix()) + downOffset)
	ratioSlope := (next.Ratio - previous.Ratio) / float64(next.Timestamp.Unix()-previous.Timestamp.Unix())
	ratioOffset := previous.Ratio - ratioSlope*float64(previous.Timestamp.Unix())
	virtualStats.Ratio = ratioSlope*float64(targetTime.Unix()) + ratioOffset
	virtualStats.Timestamp = targetTime
	virtualStats.TimestampUnix = targetTime.Unix()
	virtualStats.Tracker = previous.Tracker
	virtualStats.SchemaVersion = currentStatsDBSchemaVersion
	return virtualStats, nil
}

// ------------------------

type StatsDelta struct {
	Tracker       string
	Timestamp     time.Time
	Up            int64
	Down          int64
	Ratio         float64
	Buffer        int64
	WarningBuffer int64
}

func CalculateDelta(first, second StatsEntry) (*StatsDelta, error) {
	// check second after first
	if !second.Timestamp.After(first.Timestamp) {
		return nil, errors.New("cannot calculate delta for out of order entries")
	}

	firstBuffer, firstWarningBuffer := first.getBufferValues()
	secondBuffer, secondWarningBuffer := second.getBufferValues()
	d := &StatsDelta{
		Tracker:       second.Tracker,
		Timestamp:     second.Timestamp,
		Up:            int64(second.Up - first.Up),
		Down:          int64(second.Down - first.Down),
		Ratio:         second.Ratio - first.Ratio,
		Buffer:        secondBuffer - firstBuffer,
		WarningBuffer: secondWarningBuffer - firstWarningBuffer,
	}
	return d, nil
}

func CalculateDeltas(entries []StatsEntry) []StatsDelta {
	var deltas []StatsDelta
	for i, e := range entries {
		if i == 0 {
			deltas = append(deltas, StatsDelta{Timestamp: e.Timestamp})
		} else {
			delta, err := CalculateDelta(entries[i-1], e)
			if err != nil {
				logthis.Error(err, logthis.VERBOSEST)
				deltas = append(deltas, StatsDelta{Timestamp: e.Timestamp})
			} else {
				deltas = append(deltas, *delta)
			}
		}
	}
	return deltas
}

// ------------------------

type SnatchStatsEntry struct {
	ID           uint32 `storm:"id,increment"`
	Tracker      string `storm:"index"`
	Size         uint64
	Number       int
	Timestamp    time.Time `storm:"index"`
	Collected    bool      `storm:"index"`
	StartOfDay   bool      `storm:"index"`
	StartOfWeek  bool      `storm:"index"`
	StartOfMonth bool      `storm:"index"`
}
