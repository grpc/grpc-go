/*
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

package msgdecoder

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestMessageDecoder(t *testing.T) {
	for _, test := range []struct {
		numFrames int
		data      []string
	}{
		{1, []string{"abc"}},                // One message per frame.
		{1, []string{"abc", "def", "ghi"}},  // Multiple messages per frame.
		{3, []string{"a", "bcdef", "ghif"}}, // Multiple messages over multiple frames.
	} {
		var want []*RecvMsg
		for _, d := range test.data {
			want = append(want, &RecvMsg{Length: len(d), Overhead: 5})
			want = append(want, &RecvMsg{Data: []byte(d)})
		}
		var got []*RecvMsg
		dcdr := NewMessageDecoder(func(r *RecvMsg) { got = append(got, r) })
		for _, fr := range createFrames(test.numFrames, test.data) {
			dcdr.Decode(fr, 0)
		}
		if !match(got, want) {
			t.Fatalf("got: %v, want: %v", got, want)
		}
	}
}

func match(got, want []*RecvMsg) bool {
	for i, v := range got {
		if !reflect.DeepEqual(v, want[i]) {
			return false
		}
	}
	return true
}

func createFrames(n int, msgs []string) [][]byte {
	var b []byte
	for _, m := range msgs {
		payload := []byte(m)
		hdr := make([]byte, 5)
		binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
		b = append(b, hdr...)
		b = append(b, payload...)
	}
	// break b into n parts.
	var result [][]byte
	batch := len(b) / n
	for len(b) != 0 {
		sz := batch
		if len(b) < sz {
			sz = len(b)
		}
		result = append(result, b[:sz])
		b = b[sz:]
	}
	return result
}
