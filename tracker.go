package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

const (
	// RED only allows 5 API calls every 10s
	allowedAPICallsByPeriod = 5
	apiCallsPeriodS         = 10

	unknownTorrentURL      = "Unknown torrent URL"
	errorLogIn             = "Error logging in: "
	errorNotLoggedIn       = "Not logged in"
	errorJSONAPI           = "Error calling JSON API: "
	errorGET               = "Error calling GET on URL, got HTTP status: "
	errorUnmarshallingJSON = "Error reading JSON: "
	errorInvalidResponse   = "Invalid response. Maybe log in again?"
)

var (
	// channel of allowedAPICallsByPeriod elements, which will rate-limit the requests
	limiter = make(chan bool, allowedAPICallsByPeriod)
)

func apiCallRateLimiter() {
	// fill the rate limiter the first time
	for i := 0; i < allowedAPICallsByPeriod; i++ {
		limiter <- true
	}
	// every apiCallsPeriodS, refill the limiter channel
	for range time.Tick(time.Second * time.Duration(apiCallsPeriodS)) {
		for i := 0; i < allowedAPICallsByPeriod; i++ {
			select {
			case limiter <- true:
			default:
				// if channel is full, do nothing
				break
			}
		}
	}
}

func callJSONAPI(client *http.Client, url string) ([]byte, error) {
	if client == nil {
		return []byte{}, errors.New(errorNotLoggedIn)
	}

	// wait for rate limiter
	<-limiter
	// get request
	resp, err := client.Get(url)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []byte{}, errors.New(errorGET + resp.Status)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []byte{}, err
	}
	// check success
	var r GazelleGenericResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return []byte{}, errors.New(errorUnmarshallingJSON + err.Error())
	}
	if r.Status != "success" {
		if r.Status == "" {
			return data, errors.New(errorInvalidResponse)
		}
		return data, errors.New("Got JSON API status: " + r.Status)
	}
	return data, nil
}

//--------------------

type GazelleTracker struct {
	client  *http.Client
	rootURL string
	userID  int
}

func (t *GazelleTracker) Login(user, password string) error {
	form := url.Values{}
	form.Add("username", user)
	form.Add("password", password)
	req, err := http.NewRequest("POST", t.rootURL+"/login.php", strings.NewReader(form.Encode()))
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	options := cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	}
	jar, err := cookiejar.New(&options)
	if err != nil {
		logThis(errorLogIn+err.Error(), NORMAL)
		return err
	}
	t.client = &http.Client{Jar: jar}
	resp, err := t.client.Do(req)
	if err != nil {
		logThis(errorLogIn+err.Error(), NORMAL)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(errorLogIn + "Returned status: " + resp.Status)
	}
	if resp.Request.URL.String() == t.rootURL+"/login.php" {
		// if after sending the request we're still redirected to the login page, something went wrong.
		return errors.New(errorLogIn + "login page returned")
	}
	return nil
}

func (t *GazelleTracker) get(url string) ([]byte, error) {
	data, err := callJSONAPI(t.client, url)
	if err != nil {
		logThis(errorJSONAPI+err.Error(), NORMAL)
		// if error, try once again after logging in again
		if loginErr := t.Login(conf.user, conf.password); loginErr == nil {
			data2, callErr := callJSONAPI(t.client, url)
			if callErr != nil {
				return nil, callErr
			}
			return data2, callErr
		} else {
			return nil, errors.New("Could not log in and send get request to " + url)
		}
	}
	return data, err
}

func (t *GazelleTracker) Download(r *Release) (string, error) {
	if r.torrentURL == "" {
		return "", errors.New(unknownTorrentURL)
	}
	response, err := t.client.Get(r.torrentURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	file, err := os.Create(r.filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = io.Copy(file, response.Body)
	return r.filename, err
}

func (t *GazelleTracker) GetStats() (*TrackerStats, error) {
	if t.userID == 0 {
		data, err := t.get(t.rootURL + "/ajax.php?action=index")
		if err != nil {
			return nil, errors.New(errorJSONAPI + err.Error())
		}
		var i GazelleIndex
		if err := json.Unmarshal(data, &i); err != nil {
			return nil, errors.New(errorUnmarshallingJSON + err.Error())
		}
		t.userID = i.Response.ID
	}
	// userStats, more precise and updated faster
	data, err := t.get(t.rootURL + "/ajax.php?action=user&id=" + strconv.Itoa(t.userID))
	if err != nil {
		return nil, errors.New(errorJSONAPI + err.Error())
	}
	var s GazelleUserStats
	if unmarshalErr := json.Unmarshal(data, &s); unmarshalErr != nil {
		return nil, errors.New(errorUnmarshallingJSON + unmarshalErr.Error())
	}
	ratio, err := strconv.ParseFloat(s.Response.Stats.Ratio, 64)
	if err != nil {
		logThis("Incorrect ratio: "+s.Response.Stats.Ratio, NORMAL)
		ratio = 0.0
	}
	// GazelleIndex to TrackerStats
	stats := &TrackerStats{
		Username:      s.Response.Username,
		Class:         s.Response.Personal.Class,
		Up:            uint64(s.Response.Stats.Uploaded),
		Down:          uint64(s.Response.Stats.Downloaded),
		Buffer:        uint64(float64(s.Response.Stats.Uploaded)/0.95) - uint64(s.Response.Stats.Downloaded),
		WarningBuffer: uint64(float64(s.Response.Stats.Uploaded)/0.6) - uint64(s.Response.Stats.Downloaded),
		Ratio:         ratio,
	}
	return stats, nil
}

func (t *GazelleTracker) GetTorrentInfo(id string) (*TrackerTorrentInfo, error) {
	data, err := t.get(t.rootURL + "/ajax.php?action=torrent&id=" + id)
	if err != nil {
		return nil, errors.New(errorJSONAPI + err.Error())
	}
	var gt GazelleTorrent
	if unmarshalErr := json.Unmarshal(data, &gt); unmarshalErr != nil {
		return nil, errors.New(errorUnmarshallingJSON + unmarshalErr.Error())
	}

	artists := []string{}
	// for now, using artists, composers, "with" categories
	for _, el := range gt.Response.Group.MusicInfo.Artists {
		artists = append(artists, el.Name)
	}
	for _, el := range gt.Response.Group.MusicInfo.With {
		artists = append(artists, el.Name)
	}
	for _, el := range gt.Response.Group.MusicInfo.Composers {
		artists = append(artists, el.Name)
	}
	label := gt.Response.Group.RecordLabel
	if gt.Response.Torrent.Remastered {
		label = gt.Response.Torrent.RemasterRecordLabel
	}
	// json for metadata
	metadataJson, err := json.MarshalIndent(gt.Response, "", "    ")
	if err != nil {
		metadataJson = data // falling back to complete json
	}
	info := &TrackerTorrentInfo{id: gt.Response.Torrent.ID, label: label, logScore: gt.Response.Torrent.LogScore, artists: artists, size: uint64(gt.Response.Torrent.Size), uploader: gt.Response.Torrent.Username, coverURL: gt.Response.Group.WikiImage, folder: gt.Response.Torrent.FilePath, fullJSON: metadataJson}
	return info, nil
}
