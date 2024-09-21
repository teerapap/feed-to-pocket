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
)

type Config struct {
	StartDate time.Time         `toml:"start_date"`
	Sources   map[string]Source `toml:"sources"`
}

type Source struct {
	Id        string    `toml:"-"`
	Name      string    `toml:"name"`
	Url       string    `toml:"url"`
	UseServer bool      `toml:"use_server"`
	StartDate time.Time `toml:"start_date,omitempty"`
}

type Item struct {
	Id       string
	Url      string
	Title    string
	Time     time.Time
	Tags     []string
	Document string
}

type NewItemConsumer = func([]Item, Source) (bool, error)

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
	saved, err := consumer(newItems, source)
	if err != nil {
		return fmt.Errorf("consuming new items: %w", err)
	}

	// Save new feed file
	if saved {
		log.Printf("Saving new feed file at %s", rssPath)
		if err := os.Rename(tmpFile.Name(), rssPath); err != nil {
			return fmt.Errorf("saving new rss file: %w", err)
		}
	}

	return nil
}

func compareFeedItems(oldFeed *gofeed.Feed, newFeed *gofeed.Feed, source Source) []Item {
	if oldFeed != nil {
		log.Printf("Comparing items - old=%d, new=%d", len(oldFeed.Items), len(newFeed.Items))
	} else {
		log.Printf("Comparing items - old=0, new=%d", len(newFeed.Items))
	}
	log.Indent()
	defer log.Unindent()

	newItems := make([]Item, 0)

	guids := make(map[string]bool)
	links := make(map[string]bool)
	if oldFeed != nil {
		for _, item := range oldFeed.Items {
			guids[item.GUID] = item.GUID != ""
			links[item.Link] = item.Link != ""
		}
	}

	for _, item := range newFeed.Items {

		if item.Link == "" {
			log.Verbosef("[%s] Item has no link", item.GUID)
			continue
		}

		output := Item{
			Id:    item.Link,
			Url:   item.Link,
			Title: item.Title,
			Tags:  []string{source.Id},
		}

		if item.PublishedParsed != nil {
			if item.PublishedParsed.Before(source.StartDate) {
				log.Verbosef("[%s] Item was published (%s) before start date (%s)", output.Id, item.PublishedParsed.UTC().Format(time.DateTime), source.StartDate.UTC().Format(time.DateTime))
				continue
			}
			output.Time = *item.PublishedParsed
		} else {
			if item.UpdatedParsed != nil {
				if item.UpdatedParsed.Before(source.StartDate) {
					log.Verbosef("[%s] Item was updated (%s) before start date (%s)", output.Id, item.UpdatedParsed.UTC().Format(time.DateTime), source.StartDate.UTC().Format(time.DateTime))
					continue
				}
				output.Time = *item.UpdatedParsed
			}
		}

		if item.GUID != "" && guids[item.GUID] {
			log.Verbosef("[%s] Item GUID matched in old feed - guid=%s", output.Id, item.GUID)
			continue
		}
		if links[item.Link] {
			log.Verbosef("[%s] Item link matched in old feed", output.Id)
			continue
		}

		if source.UseServer {
			output.Document = buildDocument(item)
		}

		newItems = append(newItems, output)
	}

	return newItems
}

func buildDocument(item *gofeed.Item) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <meta charset="UTF-8">
</head>
<body>%s</body>
</html>
		`, item.Title, item.Description)
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
