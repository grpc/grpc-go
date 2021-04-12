package cdsbalancer

import "testing"

func (s) TestClusterHandler(t *testing.T) {

}

// Need a test xds client interface. Or is this provided? This will provide control over what is sent back
type FakeXDSClient struct {

}

// Implement the inte