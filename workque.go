//Copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warranties of any kind.  Use at your own risk.

// package digdispatch is originally written to create a ROS style topic system for a robot rover.
// The basic idea is that there are 2 different topics that any endpoint can publish/subscribe:
//	One is to send commands to a robot, while the other is to send sensor data back to control agents.

// Definitions:
//	A "controller" is a client node of some sort, that speaks to the robot
//	A "robot" is a client node with actuators and sensors.
// 	A robot may be a controller for another robot or even itself.

// Theory of operation:
//	Robots and Controllers use a pub/sub model like ROS.  Nothing is a "service" in the ROS sense -- everything goes through this channel.
// 	The same code will be on both Client and Server.

// 	Due to network issues, a second go-routine manages the subscribition notifications.

// Example Workflow:
//	We have "tank" robot called "robot1"  (note, this will likely be a GUID in the future)
//	We have a "UX" controller called "controller1" (also likely a GUID in the future)
//	Robot1 connects via websocket to the queue manager.  It subscribes to a dispatch topic "DriveControl".
//	Controller1 connects via a websocket to the queue manager.  It publishes to a dispatch topic "tank" specifying a target of "robot1".
//	Publish is asynch -- it simply adds the most recent dispatch to the map.
//	A drainer sends a dispatch to the websocket connected to robot1.
//	Robot1 recieves the dispatch and processes it

package digdispatch

import (
	"encoding/json"
	"fmt"
	"time"
)

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// NetworkedTopicMap represents nodes in a connection graph and what topics those nodes want notifications for.
type NetworkedTopicMap map[string][]string

// TimeShake is  a time handshake, used to track when an object gets recieved.
type TimeShake struct {
	EnquedTime       time.Time // when did we enque this?
	AcknowledgedTime time.Time // when did we acknowledge the task?
}

// MessageMetaData hold metadata about any message, can be composited to anything
// that may need this data for processing.
type MessageMetaData struct {
	Sender        string    // who sent this?
	Topic         string    // What are we informing?
	TemporalShake TimeShake // when did things happen?
	IsPickedUp    bool      // Has this been picked up?
}

// Message is the interchange type.  Everything should .
type Message struct {
	MetaData      MessageMetaData // info to process the message
	MessageBuffer []byte          // the message itself
}

// WorkQueue actually manages what each robot is doing/saying...
type WorkQueue struct {
	Publishers  NetworkedTopicMap   // A map of connected IDs and a list of topics they will publish
	Subscribers NetworkedTopicMap   // A map of connected IDs and a list what topics they want messages about.
	Messages    map[string]*Message // all messages from all robots, key is catenation  of (sender+topic)
}

// Serializable requires that all message data can go to/from byte slices
type Serializable *interface {
	ToBytes() []byte
	FromBytes([]byte)
}

// DriveCommand is Serializable, sent from controllers to robots.
type DriveCommand struct {
	Command string // what do we want to send?
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------
// All Serializable Type conversion functions here

// ToBytes required
func (robot *DriveCommand) ToBytes() []byte {
	robotBytes, conversionErr := json.Marshal(robot)
	if conversionErr == nil {
		return robotBytes
	}
	return nil
}

// FromBytes finishes up the Serializable interface
func (robot *DriveCommand) FromBytes(stream []byte) {
	conversionErr := json.Unmarshal(stream, robot)
	if conversionErr != nil {
		robot = new(DriveCommand) // we couldn't convert, so make a blank one.
	}
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

//-----------------------------------------------------------------------------------------------

// NewTimeShake creates a timeshake instance and returns it.
func NewTimeShake() TimeShake {
	Created := TimeShake{EnquedTime: time.Now()}
	return Created
}

// NewMessageMetaData Makes a new, initialized message metadata item.
func NewMessageMetaData() MessageMetaData {
	retVal := MessageMetaData{Sender: "", Topic: "", TemporalShake: NewTimeShake(), IsPickedUp: false}
	return retVal
}

// GetKeys provides the network "ids" of nodes. in the connetion graph.
func (myMap NetworkedTopicMap) GetKeys() []string {
	keys := make([]string, 0, len(myMap))
	for k := range myMap {
		keys = append(keys, k)
	}
	return keys
}

// NewMessage creates is a simpification function to create a Message with fields
func NewMessage(sender string, topic string, buffer []byte) *Message {
	retVal := new(Message)
	retVal.MetaData = MessageMetaData{Sender: sender, Topic: topic, TemporalShake: NewTimeShake()}
	retVal.MessageBuffer = buffer
	return retVal
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// Message functions.

// ToBytes creates a bytestream from this structure.
func (thisMessage Message) ToBytes() ([]byte, error) {
	return (json.Marshal(thisMessage))
}

// TryParseMessage attempts to convert the bytestream to a message.
func TryParseMessage(byteStream []byte) (*Message, error) {
	tempRetval := new(Message)
	unmarsErr := json.Unmarshal(byteStream, tempRetval)
	return tempRetval, unmarsErr
}

// Pickup sets the pickup time of a message
func (thisMessage *Message) Pickup() Message {
	retValue := Message{MetaData: thisMessage.MetaData, MessageBuffer: thisMessage.MessageBuffer}
	retValue.MetaData.TemporalShake.AcknowledgedTime = time.Now()
	retValue.MetaData.IsPickedUp = true

	return retValue
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// WorkQue functions
//-----------------------------------------------------------------------------------------------

// Init initializes all the information we need.
func (workItems *WorkQueue) Init() bool {
	workItems.Publishers = make(NetworkedTopicMap, 0)
	workItems.Subscribers = make(NetworkedTopicMap, 0)
	workItems.Messages = make(map[string]*Message, 0)

	go workItems.prepareTrash()
	return true
}

//-----------------------------------------------------------------------------------------------

// AddSubscriber adds a subscriber to the list of who to notify when.
func (workItems *WorkQueue) AddSubscriber(Notify string, From string, Topic string) {
	SubScriptionTopic := From + "/" + Topic
	workItems.Subscribers[Notify] = append(workItems.Subscribers[Notify], SubScriptionTopic)
}

//-----------------------------------------------------------------------------------------------

// ReceiveData takes a bytestream, figures out what it is, and adds to appropriate queue.
func (workItems *WorkQueue) ReceiveData(stream []byte) {
	// Steps:
	// 	Make a message.
	//	Put into the workqueue structs.

	msg, msgErr := TryParseMessage(stream)
	if msgErr != nil {
		fmt.Println(msgErr)
	} else {
		msgKey := msg.MetaData.Sender + "/" + msg.MetaData.Topic
		workItems.Messages[msgKey] = msg
	}
}

//-----------------------------------------------------------------------------------------------

// PickupMessagesForSubscriber checks the queue for any messages the subscriber has, returns a slice of them.
func (workItems *WorkQueue) PickupMessagesForSubscriber(subscriber string) []Message {
	retMessages := []Message{} // a blank return message.

	if topics, topicFound := workItems.Subscribers[subscriber]; topicFound {
		for _, topic := range topics {
			retMessages = append(retMessages, workItems.Messages[topic].Pickup())
		}
	}
	return retMessages
}

//-----------------------------------------------------------------------------------------------

// prepareTrash is not an exported function, and it constantly reshapes the que to attempt to delete picked up messages
func (workItems *WorkQueue) prepareTrash() {
	// always run
	for {
		// find all messages.
		for k, msgPtr := range workItems.Messages {
			if msgPtr.MetaData.IsPickedUp {
				// delete the item
				workItems.Messages[k] = nil
			}
		}
		// sleep this go-routine for 2 seconds
		time.Sleep(2 * time.Second)
	}
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------
