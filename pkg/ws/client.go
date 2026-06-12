package ws

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	refCount atomic.Int32
	closed   atomic.Bool
	cfg      Config
}

type Config struct {
	Endpoint        string
	Token           string
	IgnoreUnsafeCert bool
	Debug           bool
}

func (c *Config) ReportWSUrl() string {
	if strings.HasPrefix(c.Endpoint, "ws://") || strings.HasPrefix(c.Endpoint, "wss://") {
		return c.Endpoint
	}
	u, _ := url.Parse(c.Endpoint)
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/ws?token=%s", scheme, u.Host, c.Token)
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	if c.cfg.IgnoreUnsafeCert {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	url := c.cfg.ReportWSUrl()
	header := http.Header{}
	header.Set("User-Agent", "Komari-Agent-Go/1.0")

	if c.cfg.Debug {
		fmt.Printf("WS connecting to: %s\n", url)
	}

	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn
	c.closed.Store(false)
	c.refCount.Store(1)
	return nil
}

func (c *Client) Acquire() {
	c.refCount.Add(1)
}

func (c *Client) Release() {
	if c.refCount.Add(-1) <= 0 {
		c.closeInternal()
	}
}

func (c *Client) Shutdown() {
	c.closed.Store(true)
}

func (c *Client) IsClosed() bool {
	return c.closed.Load()
}

func (c *Client) closeInternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) Close() {
	c.closed.Store(true)
	c.closeInternal()
}

func (c *Client) WriteText(data []byte) error {
	if c.closed.Load() {
		return io.ErrClosedPipe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return io.ErrClosedPipe
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) WritePing() error {
	if c.closed.Load() {
		return io.ErrClosedPipe
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return io.ErrClosedPipe
	}
	return c.conn.WriteMessage(websocket.PingMessage, nil)
}

func (c *Client) ReadText() ([]byte, error) {
	if c.closed.Load() {
		return nil, io.ErrClosedPipe
	}
	for {
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if msgType == websocket.PingMessage {
			c.mu.Lock()
			c.conn.WriteMessage(websocket.PongMessage, nil)
			c.mu.Unlock()
			continue
		}
		if msgType == websocket.PongMessage {
			continue
		}
		if msgType == websocket.TextMessage {
			return data, nil
		}
	}
}
