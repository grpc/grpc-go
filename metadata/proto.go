package metadata

import (
	"strings"

	"github.com/golang/protobuf/proto"
)

// WithProto returns new metadata that is the given metadata *plus* the given
// message. The canonical form of a proto message as metadata is a metadata
// key of "<fqn>-bin" (where <fqn> is the fully-qualified name of the message)
// and value that is the binary encoding of the message.
func WithProto(md MD, msg proto.Message) (MD, error) {
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	protoMD := Pairs(proto.MessageName(msg)+binHdrSuffix, string(b))
	return Join(md, protoMD), nil
}

// GetProto populates the given message from its value in the given metadata. If
// an error occurs, it is returned. If the message was found in the metadata (and
// thus the given message is populated), this returns true; otherwise, if the
// message was not found, it returns false.
func GetProto(md MD, msg proto.Message) (bool, error) {
	key := strings.ToLower(proto.MessageName(msg) + binHdrSuffix)
	vals := md[key]
	if len(vals) == 0 {
		return false, nil
	}
	val := vals[len(vals)-1] // just use last one if there were more than one
	key, val, err := DecodeKeyValue(key, val)
	if err != nil {
		return false, err
	}
	err = proto.Unmarshal([]byte(val), msg)
	if err != nil {
		return false, err
	}
	return true, nil
}
