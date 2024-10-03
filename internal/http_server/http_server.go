//
// http_server.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap@treeboxsolutions.com>
//
// Distributed under terms of the MIT license.
//

package http_server

import (
	"context"
	"crypto/md5"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/teerapap/feed-to-pocket/internal/log"
	"github.com/teerapap/feed-to-pocket/internal/util"
)

type Config struct {
	ListenAddr string  `toml:"listen"`
	BaseUrl    string  `toml:"base_url"`
	baseUrl    url.URL // parsed BaseUrl
	RandomUrl  bool    `toml:"random_url,omitempty"`
}

type Server struct {
	Config   Config
	Srv      http.Server
	stopped  chan error
	Contents map[string]*Content
}

type Content struct {
	Id       string
	Document string
	FullUrl  string
	Done     chan error
}

func NewServer(conf Config) (*Server, error) {
	if err := conf.baseUrl.UnmarshalBinary([]byte(conf.BaseUrl)); err != nil {
		return nil, fmt.Errorf("http_server.base_url is not valid: %w", err)
	}

	log.Infof("Starting content HTTP server on %s", conf.ListenAddr)
	server := &Server{
		Config:   conf,
		Contents: make(map[string]*Content, 0),
		stopped:  make(chan error),
	}

	// Try to bind address
	l, err := net.Listen("tcp", conf.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listening to socket: %w", err)
	}

	// Handlers
	http.HandleFunc("GET /content/", func(w http.ResponseWriter, r *http.Request) {
		log.Verbosef("Received GET content request: %s", r.URL.Path)

		// get key querystring value
		hashId, htmlExt := strings.CutSuffix(path.Base(r.URL.Path), ".html")
		if !htmlExt {
			http.NotFound(w, r)
			return
		}
		content := server.Contents[hashId]
		if content == nil {
			http.NotFound(w, r)
			return
		}

		fmt.Fprint(w, content.Document)
		log.Infof("Content is served: %s", content.Id)
		select {
		case content.Done <- nil:
		default:
		}
	})

	go func() {
		err := server.Srv.Serve(l)
		if err != http.ErrServerClosed {
			log.Errorf("listening and serve http content: %v", err)
			server.stopped <- err
		}
		close(server.stopped)
	}()
	log.Infof("Started content HTTP server on %s", conf.ListenAddr)

	return server, nil
}

func (hc *Server) ServeContent(id string, document string) *Content {
	var hashId string
	if hc.Config.RandomUrl {
		hashId = fmt.Sprintf("%x", md5.Sum([]byte(id+util.RandString(8))))
	} else {
		hashId = fmt.Sprintf("%x", md5.Sum([]byte(id)))
	}
	fullUrl := hc.Config.baseUrl.JoinPath("content", hashId+".html")

	c := &Content{
		Id:       id,
		FullUrl:  fullUrl.String(),
		Document: document,
		Done:     make(chan error, 1),
	}
	hc.Contents[hashId] = c
	log.Infof("Serving content %s at %s", id, fullUrl)
	return c
}

func (hc *Server) Shutdown() error {
	log.Info("Shutting down content HTTP server")
	if err := hc.Srv.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	log.Info("Shut down content HTTP server")

	return <-hc.stopped
}
