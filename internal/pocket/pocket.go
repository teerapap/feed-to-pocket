//
// pocket.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap.c@gmail.com>
//
// Distributed under terms of the MIT license.
//

package pocket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/teerapap/feed-to-pocket/internal/log"
)

type Config struct {
	ConsumerKey string `toml:"consumer_key"`
	AccessToken string `toml:"access_token"`
	Batch       int    `toml:"batch"`
}

type Client struct {
	Config Config
}

func NewClient(config Config) (*Client, error) {
	return &Client{
		Config: config,
	}, nil
}

type NewItem struct {
	Url   string   `json:"url"`
	Title string   `json:"title,omitempty"`
	Time  int64    `json:"time,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	RefId string   `json:"ref_id,omitempty"`
}

func (item NewItem) MarshalJSON() ([]byte, error) {
	type tmpItem NewItem
	return json.Marshal(struct {
		tmpItem
		Action string `json:"action"`
	}{
		tmpItem: tmpItem(item),
		Action:  "add",
	})
}

func (c *Client) AddItems(items []NewItem) error {
	if len(items) == 0 {
		return nil
	}
	batch := c.Config.Batch
	if batch <= 0 {
		batch = 20
	}
	log.Printf("Adding %d new items to Pocket", len(items))

	for i := 0; i < len(items); i = i + batch {
		bItems := items[i:min(i+batch, len(items))]
		if batch <= len(items) {
			log.Printf("(Batch %d) %d items", (i/batch)+1, len(bItems))
		}
		body := struct {
			ConsumerKey string    `json:"consumer_key"`
			AccessToken string    `json:"access_token"`
			Actions     []NewItem `json:"actions"`
		}{
			ConsumerKey: c.Config.ConsumerKey,
			AccessToken: c.Config.AccessToken,
			Actions:     bItems,
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request in json: %w", err)
		}
		if err := c.send(jsonBody); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) send(jsonBody []byte) error {
	log.Indent()
	defer log.Unindent()
	log.Verbosef("Request Body: %s", string(jsonBody))

	req, err := http.NewRequest("POST", "https://getpocket.com/v3/send", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("creating api request in json: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("api request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Errorf("Response status code: %d", resp.StatusCode)
		for key, value := range resp.Header {
			log.Errorf("Response header[%s]: %s", key, value)
		}
		return fmt.Errorf("api response failure")
	}

	return nil
}
