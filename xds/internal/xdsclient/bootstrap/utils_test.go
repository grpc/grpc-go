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

package bootstrap

import (
	"reflect"
	"testing"
)

func TestAppendIfNotPresent(t *testing.T) {
	type args struct {
		slice []string
		elems []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{name: "empty", args: args{[]string{}, []string{"a"}}, want: []string{"a"}},
		{name: "just append", args: args{[]string{"a"}, []string{"b"}}, want: []string{"a", "b"}},
		{name: "ignore a", args: args{[]string{"a"}, []string{"a", "b"}}, want: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppendIfNotPresent(tt.args.slice, tt.args.elems...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendIfNotPresent() = %v, want %v", got, tt.want)
			}
		})
	}
}
