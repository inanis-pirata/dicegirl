package main

import "encoding/json"

// Payload structs for the small slice of the QQ gateway protocol the bot
// uses: the WebSocket envelope, READY and the two message events.

// envelope is the outer WebSocket payload (opcode 0 dispatch carries t/d/s).
type envelope struct {
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d"`
	Seq  *int64          `json:"s"`
	Type string          `json:"t"`
	ID   string          `json:"id"` // event id (outermost), used for replies
}

// readyData is d for the READY dispatch.
type readyData struct {
	SessionID string `json:"session_id"`
	Version   int    `json:"version"`
	User      struct {
		ID   string `json:"id"`
		Name string `json:"username"`
	} `json:"user"`
}

// helloData is d for opcode 10.
type helloData struct {
	HeartbeatInterval int64 `json:"heartbeat_interval"`
}

// identifyData is d for opcode 2.
type identifyData struct {
	Token   string `json:"token"`
	Intents int    `json:"intents"`
	Shard   [2]int `json:"shard"`
}

// resumeData is d for opcode 6.
type resumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

// author is the message event author. Group events populate member_openid;
// C2C events populate user_openid. union_openid, when present, is stable
// across the two scenes for the same person and lets us map a group member
// to the user_openid needed to send them a private hidden-roll result.
type author struct {
	MemberOpenid string `json:"member_openid"`
	UserOpenid   string `json:"user_openid"`
	UnionOpenid  string `json:"union_openid"`
}

// messageEvent is the shared shape of GROUP_AT_MESSAGE_CREATE and
// C2C_MESSAGE_CREATE; fields absent in one scene stay zero-valued.
type messageEvent struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	GroupOpenid string `json:"group_openid"`
	Author      author `json:"author"`
	MessageType int    `json:"message_type"`
}

const (
	eventGroupAtMessage = "GROUP_AT_MESSAGE_CREATE"
	eventC2CMessage     = "C2C_MESSAGE_CREATE"
)

// Scope constants for sendReply and the bot manager.
const (
	botScopeGroup = "groups"
	botScopeC2C   = "c2c"
)
