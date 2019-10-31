//Copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warranties of any kind.  Use at your own risk.

package digdispatch

import (
	"flag"
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
}

// Init initializes the type/struct
func (webhook *Weblink) Init(webServer string) {
	var server string
	if flag.Lookup("server") == nil {
		flag.StringVar(&server, "server", webServer, "server address")
	}
	webhook.URL = url.URL{
		Scheme: "ws",
		Host:   server,
		Path:   "/ws",
	}

}

// Connect starts the Connection
func (webhook *Weblink) Connect(header http.Header) {
	conn, _, err := websocket.DefaultDialer.Dial(webhook.URL.String(), header)
	check(err)
	webhook.Conn = conn
}

// WriteText sends a text message to the server
func (webhook *Weblink) WriteText(message chan []byte) {
	for {
		data, _ := <-message // channel reads have a tuple of the payload and an opened state always in channels.
		err := webhook.Conn.WriteMessage(websocket.TextMessage, data)
		check(err)
	}
}

// RunActionListener listens for messages
func (webhook *Weblink) RunActionListener(queueInsance *ActionQueue) {
	for {
		if webhook.Conn != nil {
			messageType, p, err := webhook.Conn.ReadMessage()
			if check(err) {
				return
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
