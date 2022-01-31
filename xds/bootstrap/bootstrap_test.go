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

type SampleCredsBuilder struct {
	gotConfig json.RawMessage
}

func (s *SampleCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, error) {
	s.gotConfig = config
	return nil, nil
}

func (s *SampleCredsBuilder) Name() string {
	return "new_creds_name"
}

func TestRegisterNew(t *testing.T) {
	// Register a new credential builder.
	s := &SampleCredsBuilder{}
	RegisterCredentials(s)

	// Create a sample JSON config.
	configMsg := "sample_config"
	rawMessage, err := json.Marshal(configMsg)
	if err != nil {
		t.Fatalf("Failed to Marshal message: %v", err)
	}

	c := GetCredentials("new_creds_name")
	if c == nil {
		t.Errorf(`GetCredentials("new_creds_name") credential = nil`)
	}

	if _, err := c.Build(rawMessage); err != nil {
		t.Errorf("Build(%v) error = %v, want nil", rawMessage, err)
	}

	var got string
	if err := json.Unmarshal(s.gotConfig, &got); err != nil {
		t.Errorf("Build gotConfig Unmarshal error = %v", err)
	}

	if want := "sample_config"; got != want {
		t.Errorf("Build config = %v, want %v", got, want)
	}

	// Create another sample JSON config.
	configMsg = "sample_another_config"
	rawMessage, err = json.Marshal(configMsg)
	if err != nil {
		t.Fatalf("Failed to Marshal message: %v", err)
	}

	_, err = c.Build(rawMessage)
	if err != nil {
		t.Errorf("Build(%v) error = %v, want nil", rawMessage, err)
	}

	if err := json.Unmarshal(s.gotConfig, &got); err != nil {
		t.Errorf("Build gotConfig Unmarshal error = %v", err)
	}

	if want := "sample_another_config"; got != want {
		t.Errorf("Build config = %v, want %v", got, want)
	}
}
