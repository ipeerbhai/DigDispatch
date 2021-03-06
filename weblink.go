//Copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warranties of any kind.  Use at your own risk.

package digdispatch

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"

	"github.com/gorilla/websocket"
)

// LocalIP is used to store local IP address in case we're called again.
var LocalIP string

// weblink is an HTTP bridge between some webhook and the local machine.
//-----------------------------------------------------------------------------------------------

// GetLocalIP returns the non loopback local IP of the host
func GetLocalIP() string {
	if len(LocalIP) > 5 {
		return LocalIP
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				LocalIP = ipnet.IP.String()
				return LocalIP
			}
		}
	}
	return ""
}

//-----------------------------------------------------------------------------------------------
func check(whatsWrong error, link *Weblink) bool {
	if whatsWrong != nil {
		link.LastErr = whatsWrong
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
	LastErr       error           // last error message.
	header        http.Header     // to hold the header we get during a connection attempt.
}

// Init initializes the type/struct
func (webhook *Weblink) Init(webServer string) {
	webhook.Server = webServer
	webhook.URL = url.URL{
		Scheme: "ws",
		Host:   webServer,
		Path:   "/ws",
	}
}

// memberConnect is unexported, and is used to hold what's needed to reconnect.
func (webhook *Weblink) memberConnect() {
	defer func() {
		r := recover()
		if r != nil {
			fmt.Println(r)
		}
	}()
	// Warning, Dial impliments fatalpanic, and cannot be recovered!  Sheesh.
	conn, _, err := websocket.DefaultDialer.Dial(webhook.URL.String(), webhook.header)
	check(err, webhook)
	webhook.Conn = conn
}

// Connect starts the Connection
func (webhook *Weblink) Connect(header http.Header) {
	webhook.header = header
	webhook.memberConnect()
}

// WriteText sends a text message to the server
func (webhook *Weblink) WriteText(message chan []byte) bool {
	ret := false
	defer func() {
		r := recover()
		if r != nil {
			fmt.Println("WriteMessage panicked")
		}
	}()
	for {
		data, _ := <-message // channel reads have a tuple of the payload and an opened state always in channels.
		err := webhook.Conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			// we might have a dropped connection.  Let's reconnect if possible, try again.
			webhook.TryReconnect()
			err = webhook.Conn.WriteMessage(websocket.TextMessage, data)
			check(err, webhook)
		}
	}
	return ret
}

// TryReconnect attempts to recontact a server if the connection is broken
func (webhook *Weblink) TryReconnect() {
	webhook.Conn.Close()
	webhook.memberConnect()
}

// RunActionListener listens for messages
func (webhook *Weblink) RunActionListener(queueInsance *ActionQueue) bool {
	for {
		nilCheck := reflect.ValueOf(webhook.Conn)
		if !nilCheck.IsNil() {
			messageType, p, err := webhook.Conn.ReadMessage()
			if check(err, webhook) {
				webhook.TryReconnect()
				messageType, p, err = webhook.Conn.ReadMessage()
				if check(err, webhook) {
					return false
				}
			}
			if messageType == websocket.TextMessage {
				if len(p) > 1 {
					// all things sent to us are messages?
					msg := fromBytes(p)
					queueInsance.ProcessMessage(msg)
				}
			}
		} else {
			// we have a nil type, break out
			return false
		}
	}
}

// Close closes the weblink.
func (webhook *Weblink) Close() {
	webhook.Conn.Close()
}
