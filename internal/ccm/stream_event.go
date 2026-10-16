package ccm

import "encoding/json"

type streamEventType uint8

const (
	unknownStreamEvent streamEventType = iota
	messageStart
	messageDelta
	messageStop
	contentBlockStart
	contentBlockDelta
	contentBlockStop
	streamPing
	streamError
)

func (t *streamEventType) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "message_start":
		*t = messageStart
	case "message_delta":
		*t = messageDelta
	case "message_stop":
		*t = messageStop
	case "content_block_start":
		*t = contentBlockStart
	case "content_block_delta":
		*t = contentBlockDelta
	case "content_block_stop":
		*t = contentBlockStop
	case "ping":
		*t = streamPing
	case "error":
		*t = streamError
	default:
		*t = unknownStreamEvent
	}
	return nil
}

type streamEvent struct {
	Type    *streamEventType `json:"type"`
	Message responseMessage  `json:"message"`
	Usage   usageDelta       `json:"usage"`
}
