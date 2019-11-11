//Copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warranties of any kind.  Use at your own risk.

package digdispatch

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

// weblink is an HTTP bridge between some webhook and the local machine.

//-----------------------------------------------------------------------------------------------
func check(whatsWrong error) bool {
	if whatsWrong != nil {
		fmt.Println(whatsWrong)
		return true
	}
	return false
}

// Weblink is the information we need to send/recieve data from the web.
type Weblink struct {
	Server        string          // the web server we need
	SecurityToken string          // a random GUID we need to always transmit on each command.
	URL           url.URL         // Whereto?
	Conn          *websocket.Conn // Let's keep this connection alive...
	header        http.Header     // to hold the header we get during a connection attempt.
}

// Init initializes the type/struct
func (webhook *Weblink) Init(webServer string) {
	webhook.URL = url.URL{
		Scheme: "ws",
		Host:   webServer,
		Path:   "/ws",
	}
}

// memberConnect is unexported, and is used to hold what's needed to reconnect.
func (webhook *Weblink) memberConnect() {
	conn, _, err := websocket.DefaultDialer.Dial(webhook.URL.String(), webhook.header)
	check(err)
	webhook.Conn = conn
}

// Connect starts the Connection
func (webhook *Weblink) Connect(header http.Header) {
	webhook.header = header
	webhook.memberConnect()
}

// WriteText sends a text message to the server
func (webhook *Weblink) WriteText(message chan []byte) {
	for {
		data, _ := <-message // channel reads have a tuple of the payload and an opened state always in channels.
		err := webhook.Conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			// we might have a dropped connection.  Let's reconnect if possible, try again.
			webhook.TryReconnect()
			err = webhook.Conn.WriteMessage(websocket.TextMessage, data)
			check(err)
		}
	}
}

// TryReconnect attempts to recontact a server if the connection is broken
func (webhook *Weblink) TryReconnect() {
	webhook.Conn.Close()
	webhook.memberConnect()
}

// RunActionListener listens for messages
func (webhook *Weblink) RunActionListener(queueInsance *ActionQueue) {
	for {
		if webhook.Conn != nil {
			messageType, p, err := webhook.Conn.ReadMessage()
			if check(err) {
				webhook.TryReconnect()
				messageType, p, err = webhook.Conn.ReadMessage()
				if check(err) {
					return
				}
			}
			if messageType == websocket.TextMessage {
				if len(p) > 1 {
					// all things sent to us are messages?
					msg := fromBytes(p)
					queueInsance.ProcessMessage(msg)
				}
			}
		}
	}
}

// Close closes the weblink.
func (webhook *Weblink) Close() {
	webhook.Conn.Close()
}
