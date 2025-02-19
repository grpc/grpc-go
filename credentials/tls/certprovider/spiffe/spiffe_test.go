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

func TestLoad(t *testing.T) {
	bundleMapFile, err := os.Open(testdata.Path("spiffe/spiffebundle.json"))
	if err != nil {
		fmt.Println(err)
	}
	defer bundleMapFile.Close()

	byteValue, _ := io.ReadAll(bundleMapFile)
	var result map[string]map[string][]byte
	json.Unmarshal([]byte(byteValue), &result)
	// var bundleMap map[spiffeid.TrustDomain]spiffebundle.Bundle
	if len(result["trust_domains"]) == 0 {
		t.Fatalf("No trust domain key")
	}
	for trustDomain, jsonBundle := range result["trust_domains"] {
		fmt.Printf("%v\n", trustDomain)
		_, err := spiffebundle.Parse(spiffeid.RequireTrustDomainFromString(trustDomain), jsonBundle)
		if err != nil {
			t.Fatalf("Error parsing bundle %v", err)
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
