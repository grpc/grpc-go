package manual_test

import (
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

func TestResolver(t *testing.T) {
	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: "address"},
		},
	})

	t.Run("update_state", func(t *testing.T) {
		defer func() {
			want := "cannot update state as grpc.Dial with resolver has not been called"
			if r := recover(); r != want {
				t.Errorf("expected panic %q, got %q", want, r)
			}
		}()
		r.UpdateState(resolver.State{Addresses: []resolver.Address{
			{Addr: "address"},
			{Addr: "anotheraddress"},
		}})
	})
	t.Run("report_error", func(t *testing.T) {
		defer func() {
			want := "cannot report error as grpc.Dial with resolver has not been called"
			if r := recover(); r != want {
				t.Errorf("expected panic %q, got %q", want, r)
			}
		}()
		r.ReportError(errors.New("example"))
	})

	_, err := grpc.Dial("whatever://localhost",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r))
	if err != nil {
		t.Errorf("dial setup error: %v", err)
	}
	r.UpdateState(resolver.State{Addresses: []resolver.Address{
		{Addr: "ok"},
	}})
	r.ReportError(errors.New("example"))
}
