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
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/teerapap/feed-to-pocket/internal/feed"
	"github.com/teerapap/feed-to-pocket/internal/log"
	"github.com/teerapap/feed-to-pocket/internal/pocket"
	"github.com/teerapap/feed-to-pocket/internal/util"
)

// Command-line Parsing
var help bool
var verbose bool
var version bool
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
	DataDir string `toml:"data_dir"`
}

type Config struct {
	Main   MainConfig    `toml:"main"`
	Pocket pocket.Config `toml:"pocket"`
	Rss    feed.Config   `toml:"rss"`
}

func main() {
	defer handleExit()

	// Parse command-line
	flag.Parse()
	log.SetVerbose(verbose)

	log.Verbosef("feed-to-pocket-%s", util.AppVersion)
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

	var conf Config
	_ = util.Must1(toml.DecodeFile(configFile, &conf))("parsing config file")

	pc := util.Must1(pocket.NewClient(conf.Pocket))("creating Pocket client")

	util.Must(feed.FindNewItems(conf.Rss, conf.Main.DataDir, func(items []pocket.NewItem, src feed.Source) error {
		log.Printf("Source (%s) found %d new items...", src.Id, len(items))
		if err := pc.AddItems(items); err != nil {
			return fmt.Errorf("calling Pocket API to add new items: %w", err)
		}
		return nil
	}))("feeding new items to pockets")

}
