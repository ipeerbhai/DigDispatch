# DigDispatch #
DigDispatch is a message system similar to the robot operating system, aka ROS.  Unlike ROS, it's designed to be cloud native, and has a different design philosophy.

## Core differences between ROS ##
* ROS has a single message core.  DigDispatch enables multiple cores as serverless instances.
* ROS uses TCP/UDP sockets, DigDispatch is built around websockets.
* ROS presumes "subscribe self", that is a node can subscribe to messages.  DigDispatch allows "subscribe other", a node can subscribe another node to messages.

## Who should use Digdispatch ##
DigDispatch is not a full ROS replacement, though it can be used as one.  ROS has features DigDispatch doesn't, such as property bags.  Digdispatch has features ROS doesn't, as mentioned above.  Use ROS for core robot development, use DigDispatch to cooridinate robots into a swarm.