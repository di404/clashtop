// Package api talks to the Clash / mihomo external controller (RESTful API).
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base   string // e.g. http://127.0.0.1:9090 (or http://unix for sockets)
	secret string
	Label  string       // human readable endpoint
	hc     *http.Client // for one-shot requests
	shc    *http.Client // no timeout, for long-lived streams
}

// New builds a client. If socket is non-empty it is used instead of addr.
func New(addr, socket, secret string) *Client {
	c := &Client{secret: secret}
	tr := &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	if socket != "" {
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
		c.base = "http://unix"
		c.Label = "unix:" + socket
	} else {
		if !strings.Contains(addr, "://") {
			addr = "http://" + addr
		}
		c.base = strings.TrimRight(addr, "/")
		c.Label = c.base
	}
	c.hc = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	c.shc = &http.Client{Transport: tr}
	return c
}

func (c *Client) req(ctx context.Context, method, path string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		r.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return r, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	r, err := c.req(ctx, http.MethodGet, path)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type Version struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	err := c.getJSON(ctx, "/version", &v)
	return v, err
}

type Metadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationIP     string `json:"destinationIP"`
	DestinationPort   string `json:"destinationPort"`
	Host              string `json:"host"`
	SniffHost         string `json:"sniffHost"`
	Process           string `json:"process"`
	ProcessPath       string `json:"processPath"`
	InboundName       string `json:"inboundName"`
	RemoteDestination string `json:"remoteDestination"`
	DNSMode           string `json:"dnsMode"`
}

type Connection struct {
	ID          string    `json:"id"`
	Metadata    Metadata  `json:"metadata"`
	Upload      int64     `json:"upload"`
	Download    int64     `json:"download"`
	Start       time.Time `json:"start"`
	Chains      []string  `json:"chains"`
	Rule        string    `json:"rule"`
	RulePayload string    `json:"rulePayload"`
}

type Connections struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        int64        `json:"memory"`
}

func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var v Connections
	err := c.getJSON(ctx, "/connections", &v)
	return v, err
}

func (c *Client) CloseConnection(ctx context.Context, id string) error {
	path := "/connections"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	r, err := c.req(ctx, http.MethodDelete, path)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(r)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("close: HTTP %d", resp.StatusCode)
	}
	return nil
}

type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

type Memory struct {
	Inuse   int64 `json:"inuse"`
	OSLimit int64 `json:"oslimit"`
}

type Log struct {
	Time    time.Time
	Level   string
	Message string
}

// rawLog accepts both the classic {type,payload} and mihomo's structured {level,message}.
type rawLog struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// stream issues a GET on a chunked JSON-lines endpoint and calls fn for each line
// until ctx is cancelled or the connection ends.
func (c *Client) stream(ctx context.Context, path string, fn func([]byte) error) error {
	r, err := c.req(ctx, http.MethodGet, path)
	if err != nil {
		return err
	}
	resp, err := c.shc.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s: stream closed", path)
}

func (c *Client) StreamTraffic(ctx context.Context, fn func(Traffic)) error {
	return c.stream(ctx, "/traffic", func(b []byte) error {
		var t Traffic
		if err := json.Unmarshal(b, &t); err != nil {
			return err
		}
		fn(t)
		return nil
	})
}

func (c *Client) StreamMemory(ctx context.Context, fn func(Memory)) error {
	return c.stream(ctx, "/memory", func(b []byte) error {
		var m Memory
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		fn(m)
		return nil
	})
}

func (c *Client) StreamLogs(ctx context.Context, level string, fn func(Log)) error {
	return c.stream(ctx, "/logs?level="+url.QueryEscape(level), func(b []byte) error {
		var r rawLog
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		l := Log{Time: time.Now(), Level: r.Type, Message: r.Payload}
		if l.Level == "" {
			l.Level = r.Level
		}
		if l.Message == "" {
			l.Message = r.Message
		}
		fn(l)
		return nil
	})
}
