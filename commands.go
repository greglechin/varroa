package main

import (
	"context"
	"net"
	"time"
)

const (
	varroaSocket = "varroa.sock"
)

func awaitOrders() {
	conn, err := net.Listen("unix", varroaSocket)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

Loop:
	for {
		c, err := conn.Accept()
		if err != nil {
			logThis("Error acceptin from unix socket: "+err.Error(), NORMAL)
			continue
		}

		buf := make([]byte, 512)
		n, err := c.Read(buf[:])
		if err != nil {
			logThis("Error reading from unix socket: "+err.Error(), NORMAL)
			continue
		}

		switch string(buf[:n]) {
		case "stats":
			go func() {
				if err := generateStats(); err != nil {
					logThis(errorGeneratingGraphs+err.Error(), NORMAL)
				}
			}()
		case "stop":
			break Loop
		case "reload":
			go func() {
				if err := loadConfiguration(); err != nil {
					logThis("Error reloading", NORMAL)
				}
			}()
		}
		c.Close()
	}
}

func generateStats() error {
	logThis("- generating stats", VERBOSE)
	return history.GenerateGraphs()
}

func loadConfiguration() error {
	newConf := &Config{}
	if err := newConf.load("config.yaml"); err != nil {
		logThis(errorLoadingConfig+err.Error(), NORMAL)
		return err
	}
	conf = newConf
	logThis(" - Configuration reloaded.", NORMAL)
	disabledAutosnatching = false
	logThis(" - Autosnatching enabled.", NORMAL)
	// if server up
	if server.Addr != "" {
		// shut down gracefully, but wait no longer than 5 seconds before halting
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logThis(errorShuttingDownServer+err.Error(), NORMAL)
		}
		// launch server again
		go webServer()
	}
	return nil
}
