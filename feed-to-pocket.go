//
// feed-to-pocket.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap.c@gmail.com>
//
// Distributed under terms of the MIT license.
//

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/teerapap/feed-to-pocket/internal/feed"
	"github.com/teerapap/feed-to-pocket/internal/http_server"
	"github.com/teerapap/feed-to-pocket/internal/log"
	"github.com/teerapap/feed-to-pocket/internal/pocket"
	"github.com/teerapap/feed-to-pocket/internal/util"
)

// Command-line Parsing
var help bool
var verbose bool
var version bool
var dryRun bool
var configFile string

func init() {
	flag.Usage = func() {
		helpUsage("")
	}
	flag.BoolVar(&help, "help", false, "Show help")
	flag.BoolVar(&help, "h", false, "Show help")
	flag.BoolVar(&verbose, "verbose", false, "Verbose output")
	flag.BoolVar(&version, "version", false, "Show version")
	flag.BoolVar(&version, "v", false, "Show version")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run mode")
	flag.StringVar(&configFile, "config", "", "Config file")
	flag.StringVar(&configFile, "c", "", "Config file")
}

func helpUsage(msg string) {
	if msg != "" {
		log.Error(msg)
	}
	fmt.Fprintf(flag.CommandLine.Output(), "%s [options] <input_pdf_file>\n", os.Args[0])
	flag.PrintDefaults()
	if msg != "" {
		os.Exit(1)
	}
}

func showVersion() {
	fmt.Printf("feed-to-pocket-%s\n", util.AppVersion)
}

// Helper functions

func handleExit() {
	if !verbose {
		if r := recover(); r != nil {
			// exit gracefully if not verbose
			log.Errorf("%s", r)
			os.Exit(1)
		}
	}
}

type MainConfig struct {
	DataDir    string             `toml:"data_dir"`
	HttpServer http_server.Config `toml:"http_server"`
}

type Config struct {
	Main   MainConfig    `toml:"main"`
	Pocket pocket.Config `toml:"pocket"`
	Rss    feed.Config   `toml:"rss,omitempty"`
}

func main() {
	log.Initialize(os.Stdout)
	defer handleExit()

	// Parse command-line
	flag.Parse()
	log.SetVerbose(verbose)

	log.Verbosef("%s", os.Args)

	if help {
		flag.Usage()
		os.Exit(0)
	} else if version {
		showVersion()
		os.Exit(0)
	} else if strings.TrimSpace(configFile) == "" {
		flag.Usage()
		os.Exit(1)
	}
	log.Infof("feed-to-pocket-%s", util.AppVersion)

	// Read config file
	var conf Config
	_ = util.Must1(toml.DecodeFile(configFile, &conf))("parsing config file")
	conf.Main.DataDir = util.Must1(filepath.Abs(conf.Main.DataDir))("checking data directory")

	// Create Pocket client
	pc := util.Must1(pocket.NewClient(conf.Pocket))("creating Pocket client")

	// Prepare http server
	var hc *http_server.Server = nil
	startServerOnce := sync.OnceValues(func() (*http_server.Server, error) {
		return http_server.NewServer(conf.Main.HttpServer)
	})

	totalItems := 0
	totalItemErrors := 0

	// Find new items from feed sources
	feed.FindNewItems(conf.Rss, conf.Main.DataDir, func(items []feed.Item, src feed.Source) (bool, error) {
		// Add to new items to Pocket
		totalItems = totalItems + len(items)
		if dryRun {
			log.Info("Skip adding to pocket because of dry-run mode")
			return false, nil
		}
		log.Indent()
		defer log.Unindent()

		scList := make([]*http_server.Content, 0)
		pItems := make([]pocket.NewItem, 0, len(items))
		for _, item := range items {
			finalUrl := item.Url
			if src.ForceArticleView {

				// Get and start http server if needed
				var err error
				hc, err = startServerOnce()
				if err != nil {
					return false, fmt.Errorf("starting content server: %w", err)
				}

				sc := hc.ServeContent(item.Id, item.Document)
				scList = append(scList, sc)
				finalUrl = sc.FullUrl
			}
			pItems = append(pItems, pocket.NewItem{
				Url:   finalUrl,
				Title: item.Title,
				Time:  item.Time.Unix(),
				Tags:  item.Tags,
			})
		}

		if err := pc.AddItems(pItems); err != nil {
			totalItemErrors = totalItemErrors + len(items)
			return false, fmt.Errorf("calling Pocket API to add new items: %w", err)
		}

		var syncAll sync.WaitGroup
		for _, sc := range scList {
			syncAll.Add(1)
			go func() {
				defer syncAll.Done()
				<-sc.Done
			}()
		}
		// wait for all servings content to be fetched once before continue
		syncAll.Wait()
		return true, nil
	})

	if hc != nil {
		if err := hc.Shutdown(); err != nil {
			log.Errorf("%s", err)
		}
	}

	log.Info("Summary:")
	log.Indent()
	log.Infof("Total %d feed sources", len(conf.Rss.Sources))
	log.Infof("Total %d new items (error=%d)", totalItems, totalItemErrors)
}
