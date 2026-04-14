package e2e

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var protoMarshaler = protojson.MarshalOptions{UseProtoNames: true}

func marshalProtoJSON(msg proto.Message) ([]byte, error) {
	return protoMarshaler.Marshal(msg)
}
