package health

//import proto "github.com/golang/protobuf/proto"
import (
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func HealthCheck(t time.Duration, cc *grpc.ClientConn, req interface{}, rep interface{}) error {
	ctx, _ := context.WithTimeout(context.Background(), t)
	//out := 
	if err := grpc.Invoke(ctx, "health.HealthCheck/HealthCheck", req, rep, cc); err != nil {
		return err
	}
	return nil
}

type HealthServer struct {
}

func (s *HealthServer) HealthCheck(ctx context.Context, in *HealthCheckRequest)(*HealthCheckResponse, error){
	out := new(HealthCheckResponse)
	return out,nil
}


