// copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warruanties of any kind.  Use at your own risk.

// package digsispatch is originally written to create a ROS style topic system for a robot rover.
// The basic idea is that there are 2 different topics that any endpoint can publish/subscribe:
//	One is to send commands to a robot, while the other is to send sensor data back to control agents.

// Definitions:
//	A "controller" is a client of some sort, that speaks to the robot
//	A "robot" is a machine with actuators and sensors.
// 	A robot may be a controller for another robot or even itself.

// Theory of operation:
//	Robots and Controllers use a pub/sub model like ROS.  Nothing is a "service" in the ROS sense -- everything goes through this channel.
//	However, we abstract differently than ROS.  We have "commands", which are fast channel and frequently looked at.  Think "RC remote joystick" information
//	And we have Information messages -- these are larger and slower.  Think "Video stream" information.
//	The "commands" are encapsulated in a dispatch structure, and the bytestreams in an InformationMessage structure.
// 	This is a push model.  Client requests a websocket, server upgrades the client.  Now, Client has a reader goroutine that gets called whenever server wants.
// 	So, there's no need for a "Get/Post" cycle.  Clients can request a PUT, but client gets pushed a "GET", doesn't "request" it.
// 	The same code will be on both Client and Server.

// 	Due to network issues, a second go-routine manages the subscribition notifications.

// Example Workflow:
//	We have "tank" robot called "robot1"  (note, this will likely be a GUID in the future)
//	We have a "UX" controller called "controller1" (also likely a GUID in the future)
//	Robot1 connects via websocket to the queue manager.  It subscribes to a dispatch topic "tank".
//	Controller1 connects via a websocket to the queue manager.  It publishes to a dispatch topic "tank" specifying a target of "robot1".
//	Publish is asynch -- it simply adds the most recent dispatch to the map.
//	A drainer sends a dispatch to the websocket connected to robot1.
//	Robot1 recieves the dispatch and processes it

package digdispatch

import (
	"encoding/json"
	"time"
)

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// Message is the bytestream representation of the structure.
type Message interface {
	ToBytes() ([]byte, error)              // for sending
	TryParse([]byte) (*interface{}, error) // for recieving
}

// TimeShake is  a time handshake, used to track when an object gets recieved.
type TimeShake struct {
	EnquedTime       time.Time // when did we enque this?
	AcknowledgedTime time.Time // when did we acknowledge the task?
}

// DispatchItem is designed as commands to the robot
type DispatchItem struct {
	Sender        string    // who requested the dispatch?
	Topic         string    // What are we dispatching?
	Command       string    // What am I asking the robot to do?
	TemporalShake TimeShake // when did things happen?
}

// InformationMessage is designed as information from the robot
type InformationMessage struct {
	Topic         string    // What are we informing?
	MessageBuffer []byte    // the message itself
	TemporalShake TimeShake // when did things happen?
}

// WorkQueue actually manages what each robot is doing/saying...
type WorkQueue struct {
	Publishers  map[string][]string           // A map of connected IDs and a list of topics they will publish
	Subscribers map[string][]string           // A map of connected IDs and a list what topics they want messages about.
	Dispatches  map[string]DispatchItem       // all dispatches for all robots, key is catenation of (robot+topic)
	Messages    map[string]InformationMessage // all messages from all robots, key is catenation  of (robot+topic)
}

//-----------------------------------------------------------------------------------------------

// NewTimeShake creates a timeshake instance and returns it.
func NewTimeShake() TimeShake {
	Created := TimeShake{EnquedTime: time.Now()}
	return Created
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// DispatchItem functions.

// ToBytes creates a bytestream from this structure.
func (thisDispatch DispatchItem) ToBytes() ([]byte, error) {
	return (json.Marshal(thisDispatch))
}

// TryParse attempts to convert the bytestream to this strcuture.
func (thisDispatch DispatchItem) TryParse(byteStream []byte) (*DispatchItem, error) {
	tempRetval := new(DispatchItem)
	unmarsErr := json.Unmarshal(byteStream, tempRetval)
	return tempRetval, unmarsErr
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// InformationMessage functions.

// ToBytes creates a bytestream from this structure.
func (thisInfo InformationMessage) ToBytes() ([]byte, error) {
	return (json.Marshal(thisInfo))
}

// TryParse attempts to convert the bytestream to this strcuture.
func (thisInfo InformationMessage) TryParse(byteStream []byte) (*InformationMessage, error) {
	tempRetval := new(InformationMessage)
	unmarsErr := json.Unmarshal(byteStream, tempRetval)
	return tempRetval, unmarsErr
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// WorkQue functions
//-----------------------------------------------------------------------------------------------

// Init initializes all the information we need.
func (workItem *WorkQueue) Init() bool {
	workItem.Publishers = make(map[string][]string, 0)
	workItem.Subscribers = make(map[string][]string, 0)
	workItem.Dispatches = make(map[string]DispatchItem, 0)
	workItem.Messages = make(map[string]InformationMessage, 0)
	return true
}

//-----------------------------------------------------------------------------------------------

// PublishDispatchItem is called by controllers adding in a command for a robot
func (workItem *WorkQueue) PublishDispatchItem(sender string, target string, topic string, command string) bool {
	// Targets can come and go due to network issues.  A watchdog is constantly pinging them.  The watchdog can remove targets that fail to ping.
	// We can't add items to a target that's not in the targets pool.

	if _, containsKey := workItem.Publishers[target]; containsKey {
		dispatch := DispatchItem{Sender: sender, Topic: topic, Command: command, TemporalShake: NewTimeShake()}
		workItem.Dispatches[target+topic] = dispatch
	} else {
		return false
	}
	return true // we added the item correctly.
}

//-----------------------------------------------------------------------------------------------

// PickupDispatchesForTaget gives a target a slice of all dispatch items waiting for that target's ID, clears the Dispatches for that target.
func (workItem *WorkQueue) PickupDispatchesForTaget(target string) ([]DispatchItem, bool) {
	// test to see if we have any dispatches for the target.
	var targetTopics []string
	var retList []DispatchItem
	foundDispatches := false

	// check to see that we have a subscription for the target, just return otherwise.
	if _, containsKey := workItem.Subscribers[target]; containsKey {
		targetTopics = make([]string, 0)
		retList = make([]DispatchItem, 0)

		// 1. Build a list of subscribed topics for the target
		for _, topic := range workItem.Subscribers[target] {
			targetTopics = append(targetTopics, topic)

		}

		// 2. check the dispatch map for items that match
		for _, topic := range targetTopics {
			if _, containsKey := workItem.Dispatches[target+topic]; containsKey {
				// we have this topic dispatch for this target.
				foundDispatches = true
				retList = append(retList, workItem.Dispatches[target+topic])
			}
		}
	}
	return retList, foundDispatches // we got nothing for that target.
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------
