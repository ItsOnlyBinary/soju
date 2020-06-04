package soju

import (
	"fmt"
	"net"
	"sync"
	"time"

	"gopkg.in/irc.v3"
)

type conn struct {
	net    net.Conn
	irc    *irc.Conn
	srv    *Server
	logger Logger

	lock     sync.Mutex
	outgoing chan<- *irc.Message
	closed   bool
}

func newConn(srv *Server, netConn net.Conn, logger Logger) *conn {
	outgoing := make(chan *irc.Message, 64)
	c := &conn{
		net:      netConn,
		irc:      irc.NewConn(netConn),
		srv:      srv,
		outgoing: outgoing,
		logger:   logger,
	}

	go func() {
		for msg := range outgoing {
			if c.srv.Debug {
				c.logger.Printf("sent: %v", msg)
			}
			c.net.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.irc.WriteMessage(msg); err != nil {
				c.logger.Printf("failed to write message: %v", err)
				break
			}
		}
		if err := c.net.Close(); err != nil {
			c.logger.Printf("failed to close connection: %v", err)
		} else {
			c.logger.Printf("connection closed")
		}
		// Drain the outgoing channel to prevent SendMessage from blocking
		for range outgoing {
			// This space is intentionally left blank
		}
	}()

	c.logger.Printf("new connection")
	return c
}

func (c *conn) isClosed() bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.closed
}

// Close closes the connection. It is safe to call from any goroutine.
func (c *conn) Close() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.closed {
		return fmt.Errorf("connection already closed")
	}

	err := c.net.Close()
	c.closed = true
	close(c.outgoing)
	return err
}

func (c *conn) ReadMessage() (*irc.Message, error) {
	msg, err := c.irc.ReadMessage()
	if err != nil {
		return nil, err
	}

	if c.srv.Debug {
		c.logger.Printf("received: %v", msg)
	}

	return msg, nil
}

// SendMessage queues a new outgoing message. It is safe to call from any
// goroutine.
//
// If the connection is closed before the message is sent, SendMessage silently
// drops the message.
func (c *conn) SendMessage(msg *irc.Message) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.closed {
		return
	}
	c.outgoing <- msg
}
