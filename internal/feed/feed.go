//
// feed.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap@treeboxsolutions.com>
//
// Distributed under terms of the MIT license.
//

package feed

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/teerapap/feed-to-pocket/internal/log"
	"github.com/teerapap/feed-to-pocket/internal/pocket"
)

// TODO: Check date/time with timezone parsing
type Config struct {
	StartDate time.Time         `toml:"start_date"`
	Sources   map[string]Source `toml:"sources"`
}

type Source struct {
	Id        string    `toml:"id"`
	Name      string    `toml:"name"`
	Url       string    `toml:"url"`
	StartDate time.Time `toml:"start_date,omitempty"`
}

type NewItemConsumer = func([]pocket.NewItem, Source) error

func FindNewItems(config Config, dataDir string, consumer NewItemConsumer) error {
	for sid, src := range config.Sources {
		if src.StartDate.IsZero() {
			src.StartDate = config.StartDate
		}
		if src.Id == "" {
			src.Id = sid
		}

		log.Printf("Processing rss source %s", src.Id)
		dir := filepath.Join(dataDir, "rss", src.Id)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("creating rss source(%s) directory: %w", src.Id, err)
		}

		err := findNewItems(src, dir, consumer)
		if err != nil {
			return fmt.Errorf("processing rss source(%s): %w", src.Id, err)
		}
	}

	return nil
}

func findNewItems(source Source, dir string, consumer NewItemConsumer) error {
	log.Indent()
	defer log.Unindent()
	// TODO: Print log with source id tag

	rssPath := filepath.Join(dir, "feed.xml")

	oldFeed, err := readExistingFeed(rssPath)
	if err != nil {
		return fmt.Errorf("reading existing rss file: %w", err)
	}

	newFeed, newFileName, err := readNewFeed(source.Url)
	if err != nil {
		return fmt.Errorf("reading new rss file: %w", err)
	}
	defer os.Remove(newFileName) // clean up

	newItems := compareFeedItems(oldFeed, newFeed, source)

	if err := consumer(newItems, source); err != nil {
		return fmt.Errorf("consuming new items: %w", err)
	}

	// save new file
	log.Printf("Saving new feed file")
	if err := os.Rename(newFileName, rssPath); err != nil {
		return fmt.Errorf("saving new rss file: %w", err)
	}
	return nil
}

func compareFeedItems(oldFeed *gofeed.Feed, newFeed *gofeed.Feed, source Source) []pocket.NewItem {
	if oldFeed != nil {
		log.Printf("Comparing items - old=%d, new=%d", len(oldFeed.Items), len(newFeed.Items))
	} else {
		log.Printf("Comparing items - old=0, new=%d", len(newFeed.Items))
	}
	log.Indent()

	newItems := make([]pocket.NewItem, 0)

	guids := make(map[string]bool)
	links := make(map[string]bool)
	if oldFeed != nil {
		for _, item := range oldFeed.Items {
			guids[item.GUID] = item.GUID != ""
			links[item.Link] = item.Link != ""
		}
	}

	defer log.Unindent()
	for _, item := range newFeed.Items {
		// TODO: Move compare logs to verbose
		var itemTime int64
		if item.PublishedParsed != nil {
			if item.PublishedParsed.Before(source.StartDate) {
				log.Printf("Item was published (%s) before start date (%s) - guid=%s, link=%s", *item.PublishedParsed, source.StartDate, item.GUID, item.Link)
				continue
			}
			itemTime = item.PublishedParsed.Unix()
		} else {
			if item.UpdatedParsed != nil {
				if item.UpdatedParsed.Before(source.StartDate) {
					log.Printf("Item was updated (%s) before start date (%s) - guid=%s, link=%s", *item.UpdatedParsed, source.StartDate, item.GUID, item.Link)
					continue
				}
				itemTime = item.UpdatedParsed.Unix()
			}
		}

		if item.GUID != "" && guids[item.GUID] {
			log.Printf("Item GUID matched with previous time - guid=%s, link=%s", item.GUID, item.Link)
			continue
		}
		if item.Link == "" {
			log.Printf("Item has no link - guid=%s, link=%s", item.GUID, item.Link)
			continue
		} else if links[item.Link] {
			log.Printf("Item link matched with previous time - guid=%s, link=%s", item.GUID, item.Link)
			continue
		}

		newItems = append(newItems, pocket.NewItem{
			Url:   item.Link,
			Title: item.Title,
			Time:  itemTime,
			Tags:  []string{source.Id},
		})
	}

	return newItems
}

func readExistingFeed(path string) (*gofeed.Feed, error) {
	log.Printf("Reading existing feed at %s", path)
	rssFile, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	defer rssFile.Close()

	fp := gofeed.NewParser()
	log.Printf("Parsing existing feed at %s", rssFile.Name())
	feed, err := fp.Parse(rssFile)
	if err != nil {
		return nil, fmt.Errorf("parsing rss file: %w", err)
	}
	return feed, nil
}

func readNewFeed(url string) (*gofeed.Feed, string, error) {
	tmpFile, err := os.CreateTemp("", "rss")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp file: %w", err)
	}
	defer tmpFile.Close()

	log.Printf("Downloading new feed at %s to %s", url, tmpFile.Name())
	if err := downloadFile(url, tmpFile); err != nil {
		os.Remove(tmpFile.Name())
		return nil, "", fmt.Errorf("downloading rss file: %w", err)
	}

	fp := gofeed.NewParser()
	log.Printf("Parsing new feed %s", tmpFile.Name())
	// TODO: Change to parse from file
	feed, err := fp.ParseURL(url)
	if err != nil {
		os.Remove(tmpFile.Name())
		return nil, "", fmt.Errorf("parsing rss file: %w", err)
	}
	return feed, tmpFile.Name(), nil
}

func downloadFile(url string, file *os.File) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad download status: %s", resp.Status)
	}

	_, err = io.Copy(file, resp.Body)
	return err
}
