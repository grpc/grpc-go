package spiffe

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc/testdata"
)

var (
	td = spiffeid.RequireTrustDomainFromString("example.com")
)

// type PartialParsedBundle struct {
// 	string TrustDomain `json:"trust_domains"`
// }

func TestLoad(t *testing.T) {
	bundleMapFile, err := os.Open(testdata.Path("spiffe/spiffebundle.json"))
	if err != nil {
		fmt.Println(err)
	}
	defer bundleMapFile.Close()

	byteValue, _ := io.ReadAll(bundleMapFile)
	var result map[string]map[string]json.RawMessage
	json.Unmarshal([]byte(byteValue), &result)
	// var bundleMap map[spiffeid.TrustDomain]spiffebundle.Bundle
	if len(result["trust_domains"]) == 0 {
		t.Fatalf("No trust domain key")
	}
	// fmt.Printf("GREG:%v\n", result)
	for trustDomain, jsonBundle := range result["trust_domains"] {
		// fmt.Printf("Json Bundle: %v\n", jsonBundle)
		// fmt.Printf("%v\n", trustDomain)
		// byteBundle, err := json.Marshal(jsonBundle)
		// if err != nil {
		// 	t.Fatalf("error marshaling bundle back to bytes %v", err)
		// }
		// TODO(GREG) issue - getting the subbytes to pass to here
		bundle, err := spiffebundle.Parse(spiffeid.RequireTrustDomainFromString(trustDomain), jsonBundle)
		if err != nil {
			t.Fatalf("Error parsing bundle %v", err)
		}
		if bundle == nil {
			t.Fatalf("blah")
		}
	}
	fmt.Printf("Done")

	// bundle, err := spiffebundle.Load(td, testdata.Path("spiffe/spiffebundle.json"))
	// if err != nil {
	// 	t.Fatalf("%v", err)
	// }
	// if bundle == nil {
	// 	t.Fatal("Bundle shouldn't be nil")
	// }

}

func TestSmallLoad(t *testing.T) {
	// bundleFile, err := os.Open(testdata.Path("spiffe/spiffe_test.json"))
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// defer bundleMapFile.Close()
	bundle, err := spiffebundle.Load(spiffeid.RequireTrustDomainFromString("example.com"), testdata.Path("spiffe/spiffe_test.json"))
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}
	if bundle == nil {
		t.Fail()
	}

}
