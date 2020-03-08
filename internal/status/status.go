package status

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

// StatusError is an alias of a status proto.  It implements error and Status,
// and a nil StatusError should never be returned by this package.
type StatusError spb.Status

func (se *StatusError) Error() string {
	p := (*spb.Status)(se)
	return fmt.Sprintf("rpc error: code = %s desc = %s", codes.Code(p.GetCode()), p.GetMessage())
}

func (se *StatusError) GRPCStatus() *spb.Status {
	return (*spb.Status)(se)
}

// Is implements future error.Is functionality.
// A statusError is equivalent if the code and message are identical.
func (se *StatusError) Is(target error) bool {
	tse, ok := target.(*StatusError)
	if !ok {
		return false
	}

	return proto.Equal((*spb.Status)(se), (*spb.Status)(tse))
}
