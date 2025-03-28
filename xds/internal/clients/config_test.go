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

package clients

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testServerIdentifierExtension struct{ x int }

func (s) TestServerIdentifierMap_Exist(t *testing.T) {
	si1 := ServerIdentifier{
		ServerURI:  "foo",
		Extensions: &testServerIdentifierExtension{1},
	}
	si2 := ServerIdentifier{
		ServerURI:  "foo",
		Extensions: &testServerIdentifierExtension{2},
	}

	tests := []struct {
		name   string
		setKey ServerIdentifier
		getKey ServerIdentifier
		exist  bool
	}{
		{
			name:   "both_empty",
			setKey: ServerIdentifier{},
			getKey: ServerIdentifier{},
			exist:  true,
		},
		{
			name:   "one_empty",
			setKey: ServerIdentifier{},
			getKey: ServerIdentifier{ServerURI: "bar"},
			exist:  false,
		},
		{
			name:   "other_empty",
			setKey: ServerIdentifier{ServerURI: "foo"},
			getKey: ServerIdentifier{},
			exist:  false,
		},
		{
			name:   "same_ServerURI",
			setKey: ServerIdentifier{ServerURI: "foo"},
			getKey: ServerIdentifier{ServerURI: "foo"},
			exist:  true,
		},
		{
			name:   "different_ServerURI",
			setKey: ServerIdentifier{ServerURI: "foo"},
			getKey: ServerIdentifier{ServerURI: "bar"},
			exist:  false,
		},
		{
			name:   "same_Extensions",
			setKey: ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			getKey: ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			exist:  true,
		},
		{
			name:   "different_Extensions",
			setKey: ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			getKey: ServerIdentifier{Extensions: testServerIdentifierExtension{2}},
			exist:  false,
		},
		{
			name:   "first_config_Extensions_is_nil",
			setKey: ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			getKey: ServerIdentifier{Extensions: nil},
			exist:  false,
		},
		{
			name:   "other_config_Extensions_is_nil",
			setKey: ServerIdentifier{Extensions: nil},
			getKey: ServerIdentifier{Extensions: testServerIdentifierExtension{2}},
			exist:  false,
		},
		{
			name: "all_fields_same",
			setKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: testServerIdentifierExtension{1},
			},
			getKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: testServerIdentifierExtension{1},
			},
			exist: true,
		},
		{
			name: "pointer_to_same_struct_same_values",
			setKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: si1,
			},
			getKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: si1,
			},
			exist: true,
		},
		{
			name: "pointer_to_same_struct_different_values",
			setKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: si1,
			},
			getKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: si2,
			},
			exist: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := map[ServerIdentifier]any{}
			m[test.setKey] = true
			_, ok := m[test.getKey]
			if test.exist != ok {
				t.Errorf("got m[test.getKey] exist = %v, want %v", ok, test.exist)
			}
		})
	}
}

func (s) TestServerIdentifierMap_Add(t *testing.T) {
	ext1 := &testServerIdentifierExtension{1}
	ext2 := &testServerIdentifierExtension{2}

	tests := []struct {
		name                 string
		existingKey          ServerIdentifier
		existingValue        any
		newKey               ServerIdentifier
		newValue             any
		wantExistingKeyValue any
		wantNewKeyValue      any
	}{
		{
			name:                 "both_empty",
			existingKey:          ServerIdentifier{},
			existingValue:        "old",
			newKey:               ServerIdentifier{},
			newValue:             "new",
			wantExistingKeyValue: "new",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "one_empty",
			existingKey:          ServerIdentifier{ServerURI: "foo"},
			existingValue:        "old",
			newKey:               ServerIdentifier{},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "other_empty",
			existingKey:          ServerIdentifier{},
			existingValue:        "old",
			newKey:               ServerIdentifier{ServerURI: "foo"},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "same_ServerURI",
			existingKey:          ServerIdentifier{ServerURI: "foo"},
			existingValue:        "old",
			newKey:               ServerIdentifier{ServerURI: "foo"},
			newValue:             "new",
			wantExistingKeyValue: "new",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "different_ServerURI",
			existingKey:          ServerIdentifier{ServerURI: "foo"},
			existingValue:        "old",
			newKey:               ServerIdentifier{ServerURI: "bar"},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "same_Extensions",
			existingKey:          ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			existingValue:        "old",
			newKey:               ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			newValue:             "new",
			wantExistingKeyValue: "new",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "different_Extensions",
			existingKey:          ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			existingValue:        "old",
			newKey:               ServerIdentifier{Extensions: testServerIdentifierExtension{2}},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "first_config_Extensions_is_nil",
			existingKey:          ServerIdentifier{Extensions: testServerIdentifierExtension{1}},
			existingValue:        "old",
			newKey:               ServerIdentifier{Extensions: nil},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name:                 "other_config_Extensions_is_nil",
			existingKey:          ServerIdentifier{Extensions: nil},
			existingValue:        "old",
			newKey:               ServerIdentifier{Extensions: testServerIdentifierExtension{2}},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		},
		{
			name: "all_fields_same",
			existingKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: testServerIdentifierExtension{1},
			},
			existingValue: "old",
			newKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: testServerIdentifierExtension{1},
			},
			newValue:             "new",
			wantExistingKeyValue: "new",
			wantNewKeyValue:      "new",
		},
		{
			name: "pointer_to_same_struct_same_values",
			existingKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: ext1,
			},
			existingValue: 1,
			newKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: ext1,
			},
			newValue:             "new",
			wantExistingKeyValue: "new",
			wantNewKeyValue:      "new",
		},
		{
			name: "pointer_to_same_struct_different_values",
			existingKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: ext1,
			},
			existingValue: "old",
			newKey: ServerIdentifier{
				ServerURI:  "foo",
				Extensions: ext2,
			},
			newValue:             "new",
			wantExistingKeyValue: "old",
			wantNewKeyValue:      "new",
		}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := map[ServerIdentifier]any{}
			m[test.existingKey] = test.existingValue
			m[test.newKey] = test.newValue
			if got, ok := m[test.existingKey]; !ok || got != test.wantExistingKeyValue {
				t.Errorf("got m[test.existingKey] = %v, want %v", got, test.wantExistingKeyValue)
			}
			if got, ok := m[test.newKey]; !ok || got != test.wantNewKeyValue {
				t.Errorf("got m[test.newKey] = %v, want %v", got, test.wantNewKeyValue)
			}
		})
	}
}
