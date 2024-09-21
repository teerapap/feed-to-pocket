//
// http_server.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap@treeboxsolutions.com>
//
// Distributed under terms of the MIT license.
//

package http_server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/teerapap/feed-to-pocket/internal/log"
)

type Config struct {
	ListenAddr string `toml:"listen"`
	BaseUrl    string `toml:"base_url"`
}

type Server struct {
	Config   Config
	Srv      http.Server
	Stop     sync.WaitGroup
	Contents map[string]*Content
}

type Content struct {
	Id       string
	Document string
	FullUrl  string
	WorkOnce sync.Once
	Work     sync.WaitGroup
}

func Start(conf Config) (*Server, error) {
	log.Infof("Starting content HTTP server on %s", conf.ListenAddr)
	server := &Server{Config: conf}
	server.Contents = make(map[string]*Content, 0)
	server.Stop.Add(1)

	// Try to bind address
	l, err := net.Listen("tcp", conf.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listening to socket: %w", err)
	}

	// Handlers
	http.HandleFunc("GET /content", func(w http.ResponseWriter, r *http.Request) {
		log.Verbosef("Received GET content request: %s", r.URL.Query())

		// get key querystring value
		key := r.URL.Query().Get("id")
		content := server.Contents[key]
		if content == nil {
			http.NotFound(w, r)
			return
		}

		fmt.Fprint(w, content.Document)
		log.Infof("Content is served: %s", content.Id)
		content.WorkOnce.Do(func() {
			content.Work.Done()
		})
	})

	go func() {
		defer server.Stop.Done()

		if err := server.Srv.Serve(l); err != http.ErrServerClosed {
			log.Errorf("listening and serve http content: %v", err)
		}
	}()
	log.Infof("Started content HTTP server on %s", conf.ListenAddr)

	return server, nil
}

func (hc *Server) ServeContent(id string, document string) *Content {
	safeId := url.QueryEscape(id)
	fullUrl := hc.Config.BaseUrl + "/content?id=" + safeId
	c := &Content{
		Id:       id,
		FullUrl:  fullUrl,
		Document: document,
	}
	c.Work.Add(1)
	hc.Contents[id] = c
	log.Infof("Serving content at %s", fullUrl)
	return c
}

func (hc *Server) Shutdown() error {
	log.Info("Shutting down content HTTP server")
	if err := hc.Srv.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}

	hc.Stop.Wait()
	return nil
}
