/*
 *
 * Copyright 2022 gRPC authors.
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
	"encoding/json"
	"testing"

	"google.golang.org/grpc/credentials"
)

var s *sampleCredsBuilder

func init() {
	// Register a new credential builder.
	s = &sampleCredsBuilder{}
	RegisterCredentials(s)
}

type sampleCredsBuilder struct {
	gotConfig json.RawMessage
}

func (s *sampleCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, error) {
	s.gotConfig = config
	return nil, nil
}

func (s *sampleCredsBuilder) Name() string {
	return "new_creds_name"
}

func TestRegisterNew(t *testing.T) {
	c := GetCredentials("new_creds_name")
	if c == nil {
		t.Fatalf(`GetCredentials("new_creds_name") credential = nil`)
	}

	rawMessage := json.RawMessage("sample_config")
	if _, err := c.Build(rawMessage); err != nil {
		t.Errorf("Build(%v) error = %v, want nil", rawMessage, err)
	}

	if got, want := string(s.gotConfig), "sample_config"; got != want {
		t.Errorf("Build config = %v, want %v", got, want)
	}
}
