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
	"sort"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/teerapap/feed-to-pocket/internal/log"
	"github.com/teerapap/feed-to-pocket/internal/pocket"
)

type Config struct {
	StartDate time.Time         `toml:"start_date"`
	Sources   map[string]Source `toml:"sources"`
}

type Source struct {
	Id        string    `toml:"-"`
	Name      string    `toml:"name"`
	Url       string    `toml:"url"`
	StartDate time.Time `toml:"start_date,omitempty"`
}

type NewItemConsumer = func([]pocket.NewItem, Source) error

func FindNewItems(config Config, dataDir string, consumer NewItemConsumer) {
	// Sort sources by id
	ids := make([]string, 0, len(config.Sources))
	for sid := range config.Sources {
		ids = append(ids, sid)
	}
	sort.Strings(ids)

	// For each source
	for _, sid := range ids {
		src := config.Sources[sid]
		if src.StartDate.IsZero() {
			src.StartDate = config.StartDate
		}
		src.Id = sid

		log.Printf("Processing rss source (%s)", src.Id)
		// Create rss source data directory
		dir := filepath.Join(dataDir, "rss", src.Id)
		if err := os.MkdirAll(dir, 0750); err != nil {
			log.Errorf("creating rss source(%s) directory: %s", src.Id, err)
		}

		// Find new items from this source
		err := findNewItems(src, dir, consumer)
		if err != nil {
			log.Errorf("processing rss source(%s): %s", src.Id, err)
		}
	}
}

func findNewItems(source Source, dir string, consumer NewItemConsumer) error {
	log.Indent()
	defer log.Unindent()

	rssPath := filepath.Join(dir, "feed.xml")

	// Read old feed
	oldFeed, err := readOldFeed(rssPath)
	if err != nil {
		return fmt.Errorf("reading old rss file: %w", err)
	}

	// Create tmp file for new feed
	tmpFile, err := os.CreateTemp("", "rss-")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) // clean up
	defer tmpFile.Close()

	// Read new feed
	newFeed, err := readNewFeed(source.Url, tmpFile)
	if err != nil {
		return fmt.Errorf("reading new rss file: %w", err)
	}

	// Compare old vs new feed items
	newItems := compareFeedItems(oldFeed, newFeed, source)

	// Consume new items
	log.Printf("Found %d new items", len(newItems))
	if len(newItems) > 0 {
		if err := func() error {
			log.Indent()
			defer log.Unindent()
			return consumer(newItems, source)
		}(); err != nil {
			return fmt.Errorf("consuming new items: %w", err)
		}
	}

	// Save new feed file
	log.Printf("Saving new feed file at %s", rssPath)
	if err := os.Rename(tmpFile.Name(), rssPath); err != nil {
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
	defer log.Unindent()

	newItems := make([]pocket.NewItem, 0)

	guids := make(map[string]bool)
	links := make(map[string]bool)
	if oldFeed != nil {
		for _, item := range oldFeed.Items {
			guids[item.GUID] = item.GUID != ""
			links[item.Link] = item.Link != ""
		}
	}

	for _, item := range newFeed.Items {
		var itemTime int64

		itemId := item.GUID
		if item.Link == "" {
			log.Verbosef("[%s] Item has no link", itemId)
			continue
		} else {
			itemId = item.Link
		}

		if item.PublishedParsed != nil {
			if item.PublishedParsed.Before(source.StartDate) {
				log.Verbosef("[%s] Item was published (%s) before start date (%s)", itemId, item.PublishedParsed.UTC().Format(time.DateTime), source.StartDate.UTC().Format(time.DateTime))
				continue
			}
			itemTime = item.PublishedParsed.Unix()
		} else {
			if item.UpdatedParsed != nil {
				if item.UpdatedParsed.Before(source.StartDate) {
					log.Verbosef("[%s] Item was updated (%s) before start date (%s)", itemId, item.UpdatedParsed.UTC().Format(time.DateTime), source.StartDate.UTC().Format(time.DateTime))
					continue
				}
				itemTime = item.UpdatedParsed.Unix()
			}
		}

		if item.GUID != "" && guids[item.GUID] {
			log.Verbosef("[%s] Item GUID matched in old feed - guid=%s", itemId, item.GUID)
			continue
		}
		if links[item.Link] {
			log.Verbosef("[%s] Item link matched in old feed", itemId)
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

func readOldFeed(path string) (*gofeed.Feed, error) {
	log.Printf("Reading old feed at %s", path)
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
	log.Printf("Parsing old feed at %s", rssFile.Name())
	feed, err := fp.Parse(rssFile)
	if err != nil {
		return nil, fmt.Errorf("parsing rss file: %w", err)
	}
	return feed, nil
}

func readNewFeed(url string, tmpFile *os.File) (*gofeed.Feed, error) {
	log.Printf("Downloading new feed from %s", url)
	if err := downloadFile(url, tmpFile); err != nil {
		return nil, fmt.Errorf("downloading rss file: %w", err)
	}

	// Reset file to head
	_, err := tmpFile.Seek(0, 0)
	if err != nil {
		return nil, fmt.Errorf("reseting tmp file: %w", err)
	}

	// Parse the downloaded file
	fp := gofeed.NewParser()
	log.Printf("Parsing new downloaded feed")
	feed, err := fp.Parse(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("parsing rss file: %w", err)
	}
	return feed, nil
}

func downloadFile(url string, file *os.File) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bad download status: %s", res.Status)
	}

	_, err = io.Copy(file, res.Body)
	return err
}
