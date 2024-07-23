/*
 *
 * Copyright 2024 gRPC authors.
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

package mem_test

import (
	"bytes"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/mem"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestSplit(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(data, func(bytes []byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	})
	checkBufData := func(b *mem.Buffer, expected []byte) {
		if !bytes.Equal(b.ReadOnlyData(), expected) {
			t.Fatalf("Buffer did not contain expected data %v, got %v", expected, b.ReadOnlyData())
		}
	}

	ref1 := buf.Ref()

	split1 := buf.Split(2)
	checkBufData(buf, data[:2])
	checkBufData(split1, data[2:])
	checkBufData(ref1, data)

	split2 := split1.Split(1)
	checkBufData(split1, data[2:3])
	checkBufData(split2, data[3:])

	splitRef := split1.Ref()
	ref2 := buf.Ref()
	split1.Free()
	buf.Free()
	ref1.Free()
	splitRef.Free()
	split2.Free()

	ready = true
	ref2.Free()

	if !freed {
		t.Fatalf("Buffer never freed")
	}
}
