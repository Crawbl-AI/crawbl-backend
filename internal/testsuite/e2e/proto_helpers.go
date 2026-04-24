package e2e

import (
	"google.golang.org/protobuf/proto"
)

func marshalProtoJSON(msg proto.Message) ([]byte, error) {
	return protoMarshaler.Marshal(msg)
}
