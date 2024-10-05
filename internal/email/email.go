//
// mail.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap.c@gmail.com>
//
// Distributed under terms of the MIT license.
//

package email

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/teerapap/feed-to-pocket/internal/log"
)

type Config struct {
	Server      string `toml:"server"`
	Username    string `toml:"username"`
	AppPassword string `toml:"app_password"`
}

type Item struct {
	Id       string
	UID      uint32
	Url      string
	Title    string
	Time     time.Time
	Tags     []string
	Document string
}

type NewItemConsumer = func([]Item) (bool, error)

func FindNewItems(config Config, consumer NewItemConsumer) error {
	if config.Server == "" {
		// disabled
		return nil
	}

	log.Printf("Read new emails from email server %s", config.Server)
	log.Indent()
	defer log.Unindent()

	c, err := client.DialTLS(config.Server, nil)
	if err != nil {
		return fmt.Errorf("failed to dial IMAP server: %w", err)
	}
	log.Printf("Connected to email server")
	defer c.Close()

	if err := c.Login(config.Username, config.AppPassword); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}
	log.Printf("Login successfully")

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return fmt.Errorf("failed to select INBOX: %w", err)
	}
	log.Printf("INBOX contains %v messages", mbox.Messages)

	if mbox.Messages > 0 {
		uids, err := c.UidSearch(imap.NewSearchCriteria())
		if err != nil {
			return fmt.Errorf("UID SEARCH command failed: %w", err)
		}
		log.Printf("UIDs matching the search criteria: %d items", len(uids))

		items, err := fetchEmailItems(c, uids)
		if err != nil {
			return fmt.Errorf("failed to fetch email items: %w", err)
		}

		saved, err := consumer(items)
		if err != nil {
			log.Errorf("consuming new email items: %w", err)
		} else if saved {
			if err := archiveItems(c, items); err != nil {
				log.Errorf("archiving consumed email items: %w", err)
			} else {
				log.Printf("Archive %d emails", len(items))
			}
		}
	}

	return nil
}

func fetchEmailItems(c *client.Client, uids []uint32) ([]Item, error) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	// Get the whole message body
	section := new(imap.BodySectionName)
	fetchItems := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope, imap.FetchFlags, imap.FetchUid, imap.FetchBodyStructure}

	messages := make(chan *imap.Message, 10)
	done := make(chan error)
	go func() {
		done <- c.UidFetch(seqset, fetchItems, messages)
	}()

	items := make([]Item, 0, len(uids))

	i := 0
	for msg := range messages {
		i = i + 1
		log.Printf("read item %d/%d", i, len(uids))
		if len(items) >= 30 {
			continue
		}
		item, err := convertEmailToItem(msg, section)
		if err != nil {
			log.Errorf("Error while converting email: %s", err)
			continue
		}

		items = append(items, item)
	}
	if err := <-done; err != nil {
		return items, fmt.Errorf("fetching messages: %w", err)
	}

	return items, nil
}

func convertEmailToItem(msg *imap.Message, section *imap.BodySectionName) (Item, error) {
	log.Indent()
	defer log.Unindent()

	var item Item

	log.Printf("Flags: %v", msg.Flags)
	log.Printf("UID: %v", msg.Uid)
	log.Printf("Subject: %v", msg.Envelope.Subject)
	log.Printf("Date: %v", msg.Envelope.Date.UTC().Format(time.DateTime))

	item.Id = msg.Envelope.MessageId
	item.UID = msg.Uid
	item.Title = msg.Envelope.Subject
	item.Time = msg.Envelope.Date

	r := msg.GetBody(section)
	if r == nil {
		return item, fmt.Errorf("server does not return message body")
	}

	body := ""

	m, err := message.Read(r)
	if message.IsUnknownCharset(err) {
		// This error is not fatal
		log.Warnf("Unknown encoding: %s", err)
	} else if err != nil {
		return item, fmt.Errorf("parsing email body: %w", err)
	}

	if mr := m.MultipartReader(); mr != nil {
		// This is a multipart message
		for e, err := mr.NextPart(); err != io.EOF; e, err = mr.NextPart() {
			kind, _, err := e.Header.ContentType()
			if err != nil {
				return item, err
			}
			if kind != "text/plain" && kind != "text/html" {
				continue
			}
			log.Printf("Multipart Kind %s", kind)
			m = e
			break
		}
	} else {
		kind, _, _ := m.Header.ContentType()
		if kind != "text/plain" && kind != "text/html" {
			return item, fmt.Errorf("unsupported email content type: %s", kind)
		}
		log.Printf("Kind: %s", kind)
	}

	c, err := io.ReadAll(m.Body)
	if err != nil {
		return item, err
	}

	body = string(c)
	matches := iftttPattern.FindStringSubmatch(body)
	if len(matches) <= 1 {
		return item, fmt.Errorf("no IFTTT Link in the body")
	}

	url := matches[1]
	log.Printf("IFTTT url: %s", url)
	finalUrl, err := findDestinationURL(url)
	if err != nil {
		return item, fmt.Errorf("failed to follow IFTTT url: %w", err)
	}
	log.Printf("Final url: %s", finalUrl)
	item.Url = finalUrl

	if body == "" {
		return item, fmt.Errorf("body not found")
	}
	return item, nil
}

var iftttPattern = regexp.MustCompile(`(?m)^via .+ (https://[^\r\n]*)[\s]*$`)

func findDestinationURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("http.Get => %w", err)
	}

	return resp.Request.URL.String(), nil
}

func archiveItems(c *client.Client, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	seqset := new(imap.SeqSet)
	for _, item := range items {
		seqset.AddNum(item.UID)
	}

	// First mark the message as deleted
	flagsOp := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}
	if err := c.UidStore(seqset, flagsOp, flags, nil); err != nil {
		return fmt.Errorf("deleting emails: %w", err)
	}

	// Then delete it
	if err := c.Expunge(nil); err != nil {
		return fmt.Errorf("expunging emails: %w", err)
	}
	return nil
}
