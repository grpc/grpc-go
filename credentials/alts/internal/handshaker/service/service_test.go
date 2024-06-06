/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package service

import (
	"testing"
)

const (
	testAddress1 = "some_address_1"
	testAddress2 = "some_address_2"
)

// TestDial verifies the behavior of the Dial function in managing gRPC connections
// and checks the below scenarios:
//   - First call to Dial with an address creates a new connection and stores it.
//   - Second call to Dial with the same address returns the existing connection
//     and expects the returned connection matching with ƒirst connection.
//   - Third call to Dial with a different address creates a separate new connection
//     and expects a different connection from connnection1 and connection2.
func TestDial(t *testing.T) {
	// First call to Dial, it should create a connection to the server running
	// at the given address.
	conn1, err := Dial(testAddress1)
	if err != nil {
		t.Fatalf("first call to Dial(%v) failed: %v", testAddress1, err)
	}
	if conn1 == nil {
		t.Fatalf("first call to Dial(%v)=(nil, _), want not nil", testAddress1)
	}
	if got, want := hsConnMap[testAddress1], conn1; got != want {
		t.Fatalf("hsConnMap[%v]=%v, want %v", testAddress1, got, want)
	}

	// Second call to Dial should return conn1 above.
	conn2, err := Dial(testAddress1)
	if err != nil {
		t.Fatalf("second call to Dial(%v) failed: %v", testAddress1, err)
	}
	if got, want := conn2, conn1; got != want {
		t.Fatalf("second call to Dial(%v)=(%v, _), want (%v,. _)", testAddress1, got, want)
	}
	if got, want := hsConnMap[testAddress1], conn1; got != want {
		t.Fatalf("hsConnMap[%v]=%v, want %v", testAddress1, got, want)
	}

	// Third call to Dial using a different address should create a new
	// connection.
	conn3, err := Dial(testAddress2)
	if err != nil {
		t.Fatalf("third call to Dial(%v) failed: %v", testAddress2, err)
	}
	if conn3 == nil {
		t.Fatalf("third call to Dial(%v)=(nil, _), want not nil", testAddress2)
	}
	if got, want := hsConnMap[testAddress2], conn3; got != want {
		t.Fatalf("hsConnMap[%v]=%v, want %v", testAddress2, got, want)
	}
	if got, want := conn2 == conn3, false; got != want {
		t.Fatalf("(conn2==conn3)=%v, want %v", got, want)
	}
}
