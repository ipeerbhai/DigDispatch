//Copyright Imran Peerbhai
// This file is licensed under the terms of the MIT license.
// This software is provided "AS-IS", and there are no warranties of any kind.  Use at your own risk.

// package digdispatch is originally written to create a ROS style topic system for a robot rover.
// The basic idea is that there are different topics that any endpoint can publish/subscribe:
//	One is to send commands to a robot, while the other is to send sensor data back to control agents.

// Definitions:
//	A "controller" is a client node of some sort, that speaks to the robot
//	A "robot" is a client node with actuators and sensors.
// 	A robot may be a controller for another robot or even itself.

// Theory of operation:thisMessage.MetaData
//	Robots and Controllers use a pub/sub model like ROS.  Nothing is a "service" in the ROS sense -- everything goes through this channel.
// 	The same code will be on both Client and Server.

// WorkQue is designed primarily for server work.
// ActionQue is designed primarily for client work.

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
	"image"
	"reflect"
	"time"

	"github.com/enriquebris/goconcurrentqueue"
)

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

const (
	ACTION_ID        = iota // ACTION_ID == The "identify" action
	ACTION_SUBSCRIBE = iota
	ACTION_PUBLISH   = iota
	ACTION_REQUEST   = iota // message is a request from client
	ACTION_RESPONSE  = iota // message is a response from the server
)

const (
	DRIVE_SET = iota // reciever should set physical motors to this state.
	DRIVE_GET = iota // reciever should report the motor state to UX.
)

// MessageQueue is a structure to handle inbound messages if a queue is needed.
var MessageQueue *goconcurrentqueue.FIFO

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
	pickupList    []string  // What subscribers have been given this already?
}

// Message is the interchange type.  Everything should .
type Message struct {
	MetaData      MessageMetaData // info to process the message
	MessageBuffer []byte          // the message itself
}

// PublishMessage manages new publications
type PublishMessage struct {
	PublishedTopic string   // who is the publisher of this message?
	MessageData    *Message // what's the message?
}

// WorkQueue actually manages what each robot is doing/saying...
type WorkQueue struct {
	PublishChannel chan PublishMessage  // a channel used to handle produce/drain of published messages
	Publishers     map[string]time.Time // A map of connected IDs and when they last published
	IDList         map[string]Identity  // a map of client names to their full identity
	Subscribers    NetworkedTopicMap    // A map of connected IDs and a list what topics they want messages about.
	Messages       map[string]*Message  // all messages from all robots, key is catenation  of (sender+topic)
}

// ActionMessage is a struct to simplify client/server communication
type ActionMessage struct {
	ActionType int      // which action is this?
	Sender     Identity // the full identity of the sender
	Payload    Message  // The needed data to handle the message.
}

// Identity describes an endpoint
type Identity struct {
	Name    string // What's the name of this object?  Can be a model number.
	LocalIP string // what's the localnet IP of the object?
}

// ActionQueue is a struct to simplify internal client dispatches
type ActionQueue struct {
	messagePump   chan []byte // the actual message pump.
	identity      Identity    // who am i?
	subscriptions map[string]func(msg *Message, params ...interface{})
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------
// Serializable and Reducable  messages
//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// Serializable requires that all message data can go to/from byte slices
type Serializable interface {
	ToBytes() []byte
	FromBytes([]byte) interface{}
}

// Reducable aka "reduce-able" is a way for a message to collapse an array to a single element.
type Reducable interface {
	AddToReductionQueue(element interface{})
	AddManyToReductionQueue(list []interface{})
	Reduce()
}

// DriveCommand is Serializable, sent from controllers to robots.
type DriveCommand struct {
	Command string // what do we want to send?
}

// DriveState is a serializable struct, used to get/set movement for a robot.
type DriveState struct {
	Direction       int // is this a set ( 0 ) or a get ( 1 )
	LeftMotorPower  int // from -100 .. 100, % power to left motor.
	RightMotorPower int // same as left
}

// ImageData is a serializable struct used to send image data around.
type ImageData struct {
	Size     image.Point // the resoltuion of the image
	Format   string      // What's the coder?
	ImgBytes []byte      // the actual image as a pure byte slice
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------
// All Serializable Type conversion functions here

// toBytes is to compensate for go's lack of generics.  It allows me to impliment the same code in many children.
func toBytes(data interface{}) []byte {
	dataBytes, conversionErr := json.Marshal(data)
	if conversionErr == nil {
		return dataBytes
	}
	return nil // nothing to do
}

//-----------------------------------------------------------------------------------------------

// frommBytes is a helper to help de-message a serializable.
func fromBytes(stream []byte) *Message {
	theMessage := new(Message)
	parseErr := json.Unmarshal(stream, theMessage)
	if parseErr != nil {
		fmt.Println(parseErr)
		return nil
	}
	return theMessage
}

//-----------------------------------------------------------------------------------------------

// ToBytes required
func (robot DriveCommand) ToBytes() []byte {
	return toBytes(robot)
}

//-----------------------------------------------------------------------------------------------

// FromBytes finishes up the Serializable interface
func (robot DriveCommand) FromBytes(stream []byte) interface{} {
	conversionErr := json.Unmarshal(stream, &robot)
	if conversionErr != nil {
		robot = *new(DriveCommand) // we couldn't convert, so make a blank one.
	}
	return robot
}

//-----------------------------------------------------------------------------------------------

// ToBytes calls a "generic" serializer.
func (robot DriveState) ToBytes() []byte {
	return toBytes(robot)
}

// FromBytes finishes up the Serializable interface
func (robot DriveState) FromBytes(stream []byte) interface{} {
	conversionErr := json.Unmarshal(stream, &robot)
	if conversionErr != nil {
		robot = *new(DriveState) // we couldn't convert, so make a blank one.
	}
	return robot
}

//-----------------------------------------------------------------------------------------------

// ToBytes allows ImageData to comply with the serializable interface.
func (img ImageData) ToBytes() []byte {
	return toBytes((img))
}

//-----------------------------------------------------------------------------------------------

// FromBytes allows ImageData to comply with the serializable interface.
func (img ImageData) FromBytes(stream []byte) interface{} {
	conversionErr := json.Unmarshal(stream, &img)
	if conversionErr != nil {
		img = ImageData{}
	}
	return img
}

//-----------------------------------------------------------------------------------------------

// ToBytes is to enable serializable of the Identity type.
func (id Identity) ToBytes() []byte {
	return toBytes(id)
}

// FromBytes is to enable serializable of the Identity type.
func (id Identity) FromBytes(stream []byte) interface{} {
	retVal := Identity{}
	conversionErr := json.Unmarshal(stream, &retVal)
	if conversionErr != nil {
		id = Identity{} // we couldn't convert, so make a blank one.
	}
	return retVal
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

//-----------------------------------------------------------------------------------------------

// TryParseMessage attempts to convert the bytestream to a message.
func TryParseMessage(byteStream []byte) (*Message, error) {
	tempRetval := new(Message)
	unmarsErr := json.Unmarshal(byteStream, tempRetval)
	return tempRetval, unmarsErr
}

//-----------------------------------------------------------------------------------------------

// Pickup sets the pickup time of a message, determines if this string is already in the pickup table.
func (thisMessage *Message) Pickup(who string) bool {
	retVal := false
	// we're already picked up and purged if we are nil.
	if thisMessage == nil {
		return true
	}

	// check the pickup list, try and purge ourselves if every reference / timeout happens.

	if thisMessage.MetaData.pickupList != nil {
		// iterate through the pickup list and see if we've got this who in it.
		for _, v := range thisMessage.MetaData.pickupList {
			if v == who {
				retVal = true
				// how long ago was this picked up?

				if time.Now().Sub(thisMessage.MetaData.TemporalShake.AcknowledgedTime).Seconds() >= 2 {
					// it's really old -- we should delete this message.
					thisMessage = nil
				}
				return retVal // no need to append -- we already know...
			}
		}
	}
	// Mark this as picked up.
	thisMessage.MetaData.TemporalShake.AcknowledgedTime = time.Now()
	thisMessage.MetaData.IsPickedUp = true

	thisMessage.MetaData.pickupList = append(thisMessage.MetaData.pickupList, who)
	return retVal
}

//-----------------------------------------------------------------------------------------------

// Copy makes a copy of a message.
func (thisMessage *Message) Copy(msg Message) {
	copyMd := MessageMetaData{Sender: msg.MetaData.Sender, Topic: msg.MetaData.Topic, TemporalShake: msg.MetaData.TemporalShake, IsPickedUp: msg.MetaData.IsPickedUp}
	copyBuffer := make([]byte, len(msg.MessageBuffer))
	copy(copyBuffer, msg.MessageBuffer)
	copied := Message{MetaData: copyMd, MessageBuffer: copyBuffer}
	thisMessage.MetaData = copied.MetaData
	thisMessage.MessageBuffer = copied.MessageBuffer
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// WorkQue functions
//-----------------------------------------------------------------------------------------------

// Init initializes all the information we need.
func (workItems *WorkQueue) Init() bool {
	workItems.Publishers = make(map[string]time.Time, 0)
	workItems.Subscribers = make(NetworkedTopicMap, 0)
	workItems.Messages = make(map[string]*Message, 0)
	workItems.PublishChannel = make(chan PublishMessage)
	workItems.IDList = make(map[string]Identity, 0)
	MessageQueue = goconcurrentqueue.NewFIFO()
	return true
}

//-----------------------------------------------------------------------------------------------

// AddSubscriber adds a subscriber to the list of who to notify when.
func (workItems *WorkQueue) AddSubscriber(Notify string, From string, Topic string) {
	if len(From) > 1 {
		Topic = From + "/" + Topic
	}
	SubScriptionTopic := Topic
	// Can only add topics that aren't already subscribed.
	// TODO: Make this more efficient.
	foundDuplicate := false
	if workItems.Subscribers[Notify] != nil {
		for _, v := range workItems.Subscribers[Notify] {
			if v == SubScriptionTopic {
				foundDuplicate = true
			}
		}

		if !foundDuplicate {
			workItems.Subscribers[Notify] = append(workItems.Subscribers[Notify], SubScriptionTopic)
		}

	} else {
		workItems.Subscribers[Notify] = append(workItems.Subscribers[Notify], SubScriptionTopic)
	}
}

//-----------------------------------------------------------------------------------------------

// ReceiveData takes a bytestream, figures out what it is, and adds to appropriate queue.
func (workItems *WorkQueue) ReceiveData(stream []byte) *ActionMessage {
	// Steps:
	// 	Make an action message
	//	Put into appropriate structs
	//	Do any needed actions.

	action, actionErr := TryParseActionMessage(stream)
	if actionErr != nil {
		fmt.Println(actionErr)
		return nil
	}
	return action
}

//-----------------------------------------------------------------------------------------------

// CreateResponse parses the action and sends an immediate response.
func (workItems *WorkQueue) CreateResponse(action *ActionMessage) ActionMessage {
	// find out who the actual sender wants to know about.
	targetName := action.Payload.MetaData.Topic

	// look up the target
	targetID := workItems.IDList[targetName]

	// create a response message
	myMetaData := MessageMetaData{Sender: "Server", Topic: "response/id", TemporalShake: NewTimeShake()}
	responseMsg := Message{MetaData: myMetaData, MessageBuffer: targetID.ToBytes()}
	responseAction := ActionMessage{ActionType: ACTION_RESPONSE, Sender: Identity{}, Payload: responseMsg}
	return responseAction
}

//-----------------------------------------------------------------------------------------------

// ExecuteAction takes an action message and modifies the work queue as needed.
func (workItems *WorkQueue) ExecuteAction(action *ActionMessage) {
	// Handle doing what's needed
	switch action.ActionType {
	case ACTION_SUBSCRIBE:
		workItems.AddSubscriber(action.Payload.MetaData.Sender, "", action.Payload.MetaData.Topic)
	case ACTION_PUBLISH:
		workItems.PublishActionMessage(action)
	}
}

//-----------------------------------------------------------------------------------------------

// PublishActionMessage sets the various structs in the workque for the drainer go-routine.
// WARNING: this routine is meant for services with a reducer goroutine to drain the queue!  Do not call this from a client!!
// This function blocks until a reducer picks up the publish message, and will stall the calling thread.
func (workItems *WorkQueue) PublishActionMessage(action *ActionMessage) {
	// create a key, get the message pointer, point WorkQueue.Messages to it.
	copiedMsg := new(Message)
	copiedMsg.Copy(action.Payload) // so garbage collector will throw away the action message.
	key := action.Payload.MetaData.Sender + "/" + action.Payload.MetaData.Topic

	// Publish the message via a channel.  Warning, this is thread blocking.
	publishThis := PublishMessage{PublishedTopic: key, MessageData: copiedMsg}
	workItems.PublishChannel <- publishThis
}

//-----------------------------------------------------------------------------------------------
//-----------------------------------------------------------------------------------------------

// ActionMessage functions

//-----------------------------------------------------------------------------------------------

// TryParseActionMessage receives a byte stream, tries to make an actionmessage from it.
func TryParseActionMessage(stream []byte) (*ActionMessage, error) {
	tempAction := new(ActionMessage)
	parseErr := json.Unmarshal(stream, tempAction)
	return tempAction, parseErr
}

//-----------------------------------------------------------------------------------------------

// ActionMessage is serializable -- it has ToBytes and FromBytes

// ToBytes -- to comply with the serializable interface.
func (webAction *ActionMessage) ToBytes(clientID string) []byte {
	return toBytes(webAction)
}

//-----------------------------------------------------------------------------------------------

// FromBytes is to comply with the Serializable interface.
func (webAction *ActionMessage) FromBytes(stream []byte) {
	tempAction := new(ActionMessage)

	parseErr := json.Unmarshal(stream, tempAction)
	if parseErr != nil {
		fmt.Println("Could not deserialize ActionMessage")
		return
	}

	webAction.ActionType = tempAction.ActionType
	webAction.Payload = tempAction.Payload
}

//-----------------------------------------------------------------------------------------------

// CreatePayload is a simple function to create a full payload from a message.
func (webAction *ActionMessage) CreatePayload(Identity string, Topic string, Buffer []byte) {
	payload := NewMessage(Identity, Topic, Buffer)
	webAction.Payload = *payload
}

//-----------------------------------------------------------------------------------------------

func (queueInstance *ActionQueue) sendMsg(webAction *ActionMessage) {
	buffer := toBytes(webAction)
	queueInstance.messagePump <- buffer
	queueInstance.messagePump <- []byte("")

}

//-----------------------------------------------------------------------------------------------

// Init prepares the action queue for work
func (queueInstance *ActionQueue) Init(messagePump chan []byte) {
	queueInstance.messagePump = messagePump
	queueInstance.identity = Identity{}
	queueInstance.subscriptions = make(map[string]func(msg *Message, params ...interface{}))
}

//-----------------------------------------------------------------------------------------------

// RequestLocalNetworkForTarget sends a request message, expects a response on the same socket.
func (queueInstance *ActionQueue) RequestLocalNetworkForTarget(target string) {
	msg := NewMessage(queueInstance.identity.Name, target, nil)
	action := ActionMessage{ActionType: ACTION_REQUEST, Payload: *msg}
	queueInstance.sendMsg(&action)
}

//-----------------------------------------------------------------------------------------------

// Identify creates an action message, which it then sends via the link.
func (queueInstance *ActionQueue) Identify(clientID string, localIP string) {
	queueInstance.identity.Name = clientID
	queueInstance.identity.LocalIP = localIP
	webAction := new(ActionMessage)
	webAction.Sender.Name = clientID
	webAction.Sender.LocalIP = localIP
	webAction.ActionType = ACTION_ID
	webAction.CreatePayload(clientID, "InternalControl", nil)
	queueInstance.sendMsg(webAction)
}

//-----------------------------------------------------------------------------------------------

// Subscribe generates a subscription message and sends it to the service,
// then holds a callback for those messages when receieved from a server.
//	Notify -- that's the clientID for my caller
//	Topic -- the topic the caller is interested in
//	callback -- the function to call when the server pushes the right message to me
func (queueInstance *ActionQueue) Subscribe(Notify string, Topic string, callback func(msg *Message, params ...interface{})) {
	// Create the action for the server and send it
	webAction := new(ActionMessage)
	webAction.ActionType = ACTION_SUBSCRIBE
	webAction.CreatePayload(Notify, Topic, nil)
	queueInstance.sendMsg(webAction)

	// Add the callback to a process que
	if callback != nil {
		key := Topic
		queueInstance.subscriptions[key] = callback

	}
}

//-----------------------------------------------------------------------------------------------

// ProcessMessage a message by Generating a key, calling the correct function callback.
func (queueInstance *ActionQueue) ProcessMessage(msg *Message) {
	if msg == nil {
		return
	}
	keyList := []string{"", msg.MetaData.Sender}
	for _, possibleKey := range keyList {
		key := possibleKey + "/" + msg.MetaData.Topic
		if queueInstance.subscriptions[key] != nil {
			queueInstance.subscriptions[key](msg)
		}
	}
}

//-----------------------------------------------------------------------------------------------

// AddCallback adds a callback to the subscription queue.
func (queueInstance *ActionQueue) AddCallback(key string, callback func(msg *Message, params ...interface{})) {
	queueInstance.subscriptions[key] = callback
}

//-----------------------------------------------------------------------------------------------

// PublishMessage actually publishes the message...
func (queueInstance *ActionQueue) PublishMessage(msg Serializable) {
	// create the action message and indicate it's a publish message.
	aMsg := new(ActionMessage)
	aMsg.ActionType = ACTION_PUBLISH

	// Get the type of the msg sent to me, and make the type name the topic.
	typeName := reflect.TypeOf(msg).Name()
	aMsg.CreatePayload(queueInstance.identity.Name, typeName, msg.ToBytes())
	queueInstance.sendMsg(aMsg)
}
