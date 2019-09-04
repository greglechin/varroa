package varroa

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goji/httpauth"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sevlyar/go-daemon"
	"gitlab.com/catastrophic/assistance/fs"
	"gitlab.com/catastrophic/assistance/logthis"
	"gitlab.com/passelecasque/obstruction/tracker"
)

const (
	downloadCommand  = "get"
	handshakeCommand = "hello"
	statsCommand     = "stats"
	autoCloseTab     = "<html><head><script>t = null;function moveMe(){t = setTimeout(\"self.close()\",5000);}</script></head><body onload=\"moveMe()\">Successfully downloaded torrent: %s</body></html>"
)

const (
	responseInfo = iota
	responseError
)

const (
	notificationArea = iota
	statsArea
)

// IncomingJSON from the websocket created by the GM script, also used with unix socket.
type IncomingJSON struct {
	Token   string
	Command string
	Args    []string
	FLToken bool
	Site    string
}

// OutgoingJSON to the websocket created by the GM script.
type OutgoingJSON struct {
	Status  int
	Target  int
	Message string
}

// TODO: see if this could also be used by irc
func manualSnatchFromID(e *Environment, t *tracker.Gazelle, id string, useFLToken bool) (*Release, error) {
	stats, err := NewStatsDB(filepath.Join(StatsDir, DefaultHistoryDB))
	if err != nil {
		return nil, errors.Wrap(err, "could not access the stats database")
	}
	// get torrent info
	info := &TrackerMetadata{}
	if err := info.LoadFromID(t, id); err != nil {
		logthis.Info(errorCouldNotGetTorrentInfo, logthis.NORMAL)
		return nil, err // probably the ID does not exist
	}
	release := info.Release()
	if release == nil {
		logthis.Info("Error parsing Torrent Info", logthis.NORMAL)
		release = &Release{Tracker: t.Name, TorrentID: id}
	}
	logthis.Info("Downloading torrent "+release.ShortString(), logthis.NORMAL)
	if err := t.DownloadTorrentFromID(id, e.config.General.WatchDir, useFLToken); err != nil {
		logthis.Error(errors.Wrap(err, errorDownloadingTorrent+id), logthis.NORMAL)
		return release, err
	}
	// add to history
	release.Filter = manualSnatchFilterName
	if err := stats.AddSnatch(*release); err != nil {
		logthis.Info(errorAddingToHistory, logthis.NORMAL)
	}
	// save metadata
	if e.config.General.AutomaticMetadataRetrieval {
		if daemon.WasReborn() {
			go info.SaveFromTracker(filepath.Join(e.config.General.DownloadDir, info.FolderName), t)
		} else {
			info.SaveFromTracker(filepath.Join(e.config.General.DownloadDir, info.FolderName), t)
		}
	}
	return release, nil
}

func validateGet(r *http.Request, config *Config) (string, string, bool, error) {
	queryParameters := r.URL.Query()
	// get torrent ID
	id, ok := mux.Vars(r)["id"]
	if !ok {
		// if it's not in URL, try to get from query parameters
		queryID, ok2 := queryParameters["id"]
		if !ok2 {
			return "", "", false, errors.New(errorNoID)
		}
		id = queryID[0]
	}
	// get site
	trackerLabel, ok := mux.Vars(r)["site"]
	if !ok {
		// if it's not in URL, try to get from query parameters
		queryTrackerLabel, ok2 := queryParameters["site"]
		if !ok2 {
			return "", "", false, errors.New(errorNoID)
		}
		trackerLabel = queryTrackerLabel[0]
	}
	// checking token
	token, ok := queryParameters["token"]
	if !ok {
		// try to get token from "pass" parameter instead
		token, ok = queryParameters["pass"]
		if !ok {
			return "", "", false, errors.New(errorNoToken)
		}
	}
	if token[0] != config.WebServer.Token {
		return "", "", false, errors.New(errorWrongToken)
	}

	// checking FL token use
	useFLToken := false
	useIt, ok := queryParameters["fltoken"]
	if ok && useIt[0] == "true" {
		useFLToken = true
		logthis.Info("Snatching using FL Token if possible.", logthis.VERBOSE)
	}
	return trackerLabel, id, useFLToken, nil
}

func webServer(e *Environment) {
	if !e.config.webserverConfigured {
		logthis.Info(webServerNotConfigured, logthis.NORMAL)
		return
	}
	var additionalSources []string
	if e.config.LibraryConfigured {
		additionalSources = e.config.Library.AdditionalSources
	}
	downloads, err := NewDownloadsDB(DefaultDownloadsDB, e.config.General.DownloadDir, additionalSources)
	if err != nil {
		logthis.Error(errors.Wrap(err, "Error loading downloads database"), logthis.VERBOSE)
	}
	if e.config.WebServer.ServeMetadata {
		// scan on startup in goroutine
		go downloads.Scan()
	}

	rtr := mux.NewRouter()
	var mutex = &sync.Mutex{}
	if e.config.WebServer.AllowDownloads {
		getStats := func(w http.ResponseWriter, r *http.Request) {
			// checking token
			token, ok := r.URL.Query()["token"]
			if !ok {
				logthis.Info(errorNoToken, logthis.NORMAL)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if token[0] != e.config.WebServer.Token {
				logthis.Info(errorWrongToken, logthis.NORMAL)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// get site
			trackerLabel, ok := mux.Vars(r)["site"]
			if !ok {
				// if it's not in URL, try to get from query parameters
				queryTrackerLabel, ok2 := r.URL.Query()["site"]
				if !ok2 {
					logthis.Info(errorNoID, logthis.NORMAL)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				trackerLabel = queryTrackerLabel[0]
			}
			// get filename
			filename, ok := mux.Vars(r)["name"]
			if !ok {
				logthis.Info(errorNoStatsFilename, logthis.NORMAL)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			file, err := ioutil.ReadFile(filepath.Join(StatsDir, trackerLabel+"_"+filename))
			if err != nil {
				logthis.Error(errors.Wrap(err, errorNoStatsFilename), logthis.NORMAL)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if strings.HasSuffix(filename, svgExt) {
				w.Header().Set("Content-type", "image/svg")
			} else {
				w.Header().Set("Content-type", "image/png")
			}
			w.Write(file)
		}
		getTorrent := func(w http.ResponseWriter, r *http.Request) {
			trackerLabel, id, useFLToken, err := validateGet(r, e.config)
			if err != nil {
				logthis.Error(errors.Wrap(err, "Error parsing request"), logthis.NORMAL)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// snatching
			tracker, err := e.Tracker(trackerLabel)
			if err != nil {
				logthis.Error(errors.Wrap(err, "Error identifying in configuration tracker "+trackerLabel), logthis.NORMAL)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			release, err := manualSnatchFromID(e, tracker, id, useFLToken)
			if err != nil {
				logthis.Error(errors.Wrap(err, ErrorSnatchingTorrent), logthis.NORMAL)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// write response
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(autoCloseTab, release.ShortString())))
		}
		getMetadata := func(w http.ResponseWriter, r *http.Request) {
			// if not configured, return error
			if !e.config.WebServer.ServeMetadata {
				logthis.Error(errors.New("Error, not configured to serve metadata"), logthis.NORMAL)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			var response []byte
			id, ok := mux.Vars(r)["id"]
			if !ok {
				list, err := e.serverData.DownloadsList(downloads)
				if err != nil {
					logthis.Error(errors.Wrap(err, "Error loading downloads list"), logthis.NORMAL)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				response = list
			} else {

				info, err := e.serverData.DownloadsInfo(e, downloads, id)
				if err != nil {
					logthis.Error(errors.Wrap(err, "Error loading downloads info"), logthis.NORMAL)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				response = info
			}
			// write response
			w.WriteHeader(http.StatusOK)
			w.Write(response)
		}
		upgrader := websocket.Upgrader{
			// allows connection to websocket from anywhere
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		socket := func(w http.ResponseWriter, r *http.Request) {
			c, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				logthis.Error(errors.Wrap(err, errorCreatingWebSocket), logthis.NORMAL)
				return
			}
			defer c.Close()
			// channel to know when the connection with a specific instance is over
			endThisConnection := make(chan struct{})

			// this goroutine will send messages to the remote
			go func() {
				logOutput := logthis.Subscribe()
				for {
					select {
					case messageToLog := <-logOutput:
						mutex.Lock()
						// TODO differentiate info / error
						if err := c.WriteJSON(OutgoingJSON{Status: responseInfo, Message: messageToLog.(string), Target: notificationArea}); err != nil {
							if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
								logthis.Error(errors.Wrap(err, errorOutgoingWebSocketJSON), logthis.VERBOSEST)
							}
							endThisConnection <- struct{}{}
							break
						}
						mutex.Unlock()
					case <-endThisConnection:
						mutex.Lock()
						logthis.Unsubscribe(logOutput)
						mutex.Unlock()
						return
					}
				}
			}()

			for {
				// TODO if server is shutting down, c.Close()
				incoming := IncomingJSON{}
				if err := c.ReadJSON(&incoming); err != nil {
					if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						logthis.Error(errors.Wrap(err, errorIncomingWebSocketJSON), logthis.VERBOSEST)
					}
					endThisConnection <- struct{}{}
					break
				}

				var answer OutgoingJSON
				if incoming.Token != e.config.WebServer.Token {
					logthis.Info(errorIncorrectWebServerToken, logthis.NORMAL)
					answer = OutgoingJSON{Status: responseError, Target: notificationArea, Message: "Bad token!"}
				} else {
					// dealing with command
					switch incoming.Command {
					case handshakeCommand:
						// say hello right back
						answer = OutgoingJSON{Status: responseInfo, Target: notificationArea, Message: handshakeCommand}
					case downloadCommand:
						tracker, err := e.Tracker(incoming.Site)
						if err != nil {
							logthis.Error(errors.Wrap(err, "Error identifying in configuration tracker "+incoming.Site), logthis.NORMAL)
							answer = OutgoingJSON{Status: responseError, Target: notificationArea, Message: "Error snatching torrent."}
						} else {
							// snatching
							for _, id := range incoming.Args {
								release, err := manualSnatchFromID(e, tracker, id, incoming.FLToken)
								if err != nil {
									logthis.Info("Error snatching torrent: "+err.Error(), logthis.NORMAL)
									answer = OutgoingJSON{Status: responseError, Target: notificationArea, Message: "Error snatching torrent."}
								} else {
									answer = OutgoingJSON{Status: responseInfo, Target: notificationArea, Message: "Successfully snatched torrent " + release.ShortString()}
								}
								// TODO send responses for all IDs (only 1 from GM Script for now anyway)
							}
						}
					case statsCommand:
						// TODO gather stats and send text (ie snatched today, this week, etc...)
						answer = OutgoingJSON{Status: responseInfo, Target: statsArea, Message: statusString(e)}
					default:
						answer = OutgoingJSON{Status: responseError, Target: notificationArea, Message: errorUnknownCommand + incoming.Command}
					}
				}
				// writing answer
				mutex.Lock()
				if err := c.WriteJSON(answer); err != nil {
					if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						logthis.Error(errors.Wrap(err, errorOutgoingWebSocketJSON+" (answer)"), logthis.VERBOSEST)
					}
					endThisConnection <- struct{}{}
					break
				}
				mutex.Unlock()
			}
		}
		// interface for remotely ordering downloads
		rtr.HandleFunc("/get/{id:[0-9]+}", getTorrent).Methods("GET")
		rtr.HandleFunc("/downloads", getMetadata).Methods("GET")
		rtr.HandleFunc("/downloads/{id:[0-9]+}", getMetadata).Methods("GET")
		rtr.HandleFunc("/getStats/{name:[\\w]+.svg}", getStats).Methods("GET")
		rtr.HandleFunc("/getStats/{name:[\\w]+.png}", getStats).Methods("GET")
		rtr.HandleFunc("/dl.pywa", getTorrent).Methods("GET")
		rtr.HandleFunc("/ws", socket)

	}
	if e.config.WebServer.ServeStats {
		getLocalStats := func(w http.ResponseWriter, r *http.Request) {
			// get filename
			filename, ok := mux.Vars(r)["name"]
			if !ok {
				logthis.Info(errorNoStatsFilename, logthis.NORMAL)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			http.ServeFile(w, r, filepath.Join(StatsDir, filename))
		}
		getIndex := func(w http.ResponseWriter, r *http.Request) {
			response, err := e.serverData.Index(downloads)
			if err != nil {
				logthis.Error(errors.Wrap(err, "Error loading downloads list"), logthis.NORMAL)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// write response
			w.WriteHeader(http.StatusOK)
			w.Write(response)
		}
		if e.config.WebServer.Password != "" {
			rtr.Handle("/", httpauth.SimpleBasicAuth(e.config.WebServer.User, e.config.WebServer.Password)(http.HandlerFunc(getIndex)))
			rtr.Handle("/{name:[\\w]+.svg}", httpauth.SimpleBasicAuth(e.config.WebServer.User, e.config.WebServer.Password)(http.HandlerFunc(getLocalStats)))
			rtr.Handle("/{name:[\\w]+.png}", httpauth.SimpleBasicAuth(e.config.WebServer.User, e.config.WebServer.Password)(http.HandlerFunc(getLocalStats)))
		} else {
			rtr.HandleFunc("/", getIndex)
			rtr.HandleFunc("/{name:[\\w]+.svg}", getLocalStats)
			rtr.HandleFunc("/{name:[\\w]+.png}", getLocalStats)
		}
	}
	// serve
	if e.config.webserverHTTP {
		go func() {
			logthis.Info(webServerUpHTTP, logthis.NORMAL)
			httpServer := &http.Server{Addr: fmt.Sprintf(":%d", e.config.WebServer.PortHTTP), Handler: rtr}
			if err := httpServer.ListenAndServe(); err != nil {
				if err == http.ErrServerClosed {
					logthis.Info(webServerShutDown, logthis.NORMAL)
				} else {
					logthis.Error(errors.Wrap(err, errorServing), logthis.NORMAL)
				}
			}
		}()
	}
	if e.config.webserverHTTPS {
		// if not there yet, generate the self-signed certificate
		if !fs.FileExists(filepath.Join(certificatesDir, certificateKey)) || !fs.FileExists(filepath.Join(certificatesDir, certificate)) {
			if err := generateCertificates(e); err != nil {
				logthis.Error(errors.Wrap(err, errorGeneratingCertificate+provideCertificate), logthis.NORMAL)
				logthis.Info(infoBackupScript, logthis.NORMAL)
				return
			}
			// basic instruction for first connection.
			logthis.Info(infoAddCertificates, logthis.NORMAL)
		}

		go func() {
			logthis.Info(webServerUpHTTPS, logthis.NORMAL)
			httpsServer := &http.Server{Addr: fmt.Sprintf(":%d", e.config.WebServer.PortHTTPS), Handler: rtr}
			if err := httpsServer.ListenAndServeTLS(filepath.Join(certificatesDir, certificate), filepath.Join(certificatesDir, certificateKey)); err != nil {
				if err == http.ErrServerClosed {
					logthis.Info(webServerShutDown, logthis.NORMAL)
				} else {
					logthis.Error(errors.Wrap(err, errorServing), logthis.NORMAL)
				}
			}
		}()
	}
	logthis.Info(webServersUp, logthis.NORMAL)
}
