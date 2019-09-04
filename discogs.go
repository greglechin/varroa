package varroa

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/passelecasque/obstruction/tracker"
	"golang.org/x/net/publicsuffix"
)

const (
	discogsUserAgent  = "ThatThing/2.4.6"
	discogsSearchURL  = "https://api.discogs.com/database/search"
	discogsReleaseURL = "https://api.discogs.com/releases/"
)

// Discogs retrieves information about a release on Discogs.
type Discogs struct {
	Token  string
	Client *http.Client
}

// NewDiscogsRelease set up with Discogs API authorization info.
func NewDiscogsRelease(token string) (*Discogs, error) {
	options := cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	}
	jar, err := cookiejar.New(&options)
	if err != nil {
		logthis.Error(errors.Wrap(err, errorLogIn), logthis.NORMAL)
		return nil, err
	}
	return &Discogs{Token: token, Client: &http.Client{Jar: jar}}, nil
}

func (d *Discogs) getRequest(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", discogsUserAgent)
	req.Header.Add("Authorization", "Discogs token="+d.Token)
	return d.Client.Do(req)
}

// GetRelease on Discogs and retrieve its information
func (d *Discogs) GetRelease(id int) (*DiscogsRelease, error) {

	resp, err := d.getRequest(fmt.Sprintf("%s/%d", discogsReleaseURL, id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Returned status: " + resp.Status)
	}

	resultBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var info DiscogsRelease
	err = json.Unmarshal(resultBytes, &info)
	return &info, err
}

func (d *Discogs) SearchFromTrackerMetadata(info TrackerMetadata) (*DiscogsResults, error) {

	// TODO: original year or edition year??
	// TODO if more than 1 artists???

	return d.Search(info.Artists[0].Name, info.Title, info.OriginalYear, info.RecordLabel, info.CatalogNumber, info.Source, info.ReleaseType)
}

// Search releases on Discogs and retrieve their information
func (d *Discogs) Search(artist, release string, year int, label, catalogNumber, source, releaseType string) (*DiscogsResults, error) {
	// search
	searchURL, err := url.Parse(discogsSearchURL)
	if err != nil {
		return nil, err
	}
	q := searchURL.Query()
	q.Set("type", "release")
	q.Set("artist", artist)
	q.Set("release_title", release)
	if year != 0 {
		q.Set("year", strconv.Itoa(year))
	}
	if label != "" {
		q.Set("label", label)
	}
	if catalogNumber != "" {
		q.Set("catno", fmt.Sprintf("\"%s\"", catalogNumber))
	}
	var format []string
	if source != "" {
		format = append(format, source)
	}
	if releaseType != "" {
		// anthology and compilation have different meanings on discogs...
		if releaseType == tracker.ReleaseAnthology {
			releaseType = tracker.ReleaseCompilation
		}
		format = append(format, releaseType)
	}
	q.Set("format", strings.Join(format, "|"))
	searchURL.RawQuery = q.Encode()

	resp, err := d.getRequest(searchURL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Returned status: " + resp.Status)
	}

	resultDCBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info DiscogsResults
	err = json.Unmarshal(resultDCBytes, &info)
	return &info, err
}
