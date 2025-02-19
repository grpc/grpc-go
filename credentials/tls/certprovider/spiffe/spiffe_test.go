package spiffe

import (
	"testing"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc/testdata"
)

var (
	td = spiffeid.RequireTrustDomainFromString("foo.bar.com")
)

func TestLoad(t *testing.T) {
	bundle, err := spiffebundle.Load(td, testdata.Path("spiffebundle.json"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if bundle == nil {
		t.Fatal("Bundle shouldn't be nil")
	}

}
