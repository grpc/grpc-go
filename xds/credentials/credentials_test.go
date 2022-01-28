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
package credentials

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func TestDefaultBundles(t *testing.T) {
	_, err := Get("google_default", nil)
	if err != nil {
		t.Errorf(`Bundle("google_default") error = %v, want nil`, err)
	}

	_, err = Get("insecure", nil)
	if err != nil {
		t.Errorf(`Bundle("insecure") error = %v, want nil`, err)
	}
}

type SampleCredsBuilder struct {
	gotConfig json.RawMessage
}

func (s *SampleCredsBuilder) BuildCredsBundle(config json.RawMessage) (credentials.Bundle, error) {
	s.gotConfig = config
	return insecure.NewBundle(), nil
}

func (s *SampleCredsBuilder) Name() string {
	return "new_creds_name"
}

func TestRegisterNew(t *testing.T) {
	// Register a new credential builder.
	s := &SampleCredsBuilder{}
	Register(s)

	// Create a sample JSON config.
	configMsg := "sample_config"
	rawMessage, err := json.Marshal(configMsg)
	if err != nil {
		t.Fatalf("Failed to Marshal message: %v", err)
	}

	_, err = Get("new_creds_name", rawMessage)
	if err != nil {
		t.Errorf(`Get("new_creds_name") error = %v, want nil`, err)
	}

	var got string
	if err := json.Unmarshal(s.gotConfig, &got); err != nil {
		t.Errorf("Get gotConfig Unmarshal error = %v", err)
	}

	if want := "sample_config"; got != want {
		t.Errorf("Get config = %v, want %v", got, want)
	}

	// Create another sample JSON config.
	configMsg = "sample_another_config"
	rawMessage, err = json.Marshal(configMsg)
	if err != nil {
		t.Fatalf("Failed to Marshal message: %v", err)
	}

	_, err = Get("new_creds_name", rawMessage)
	if err != nil {
		t.Errorf(`Get("new_creds_name") error = %v, want nil`, err)
	}

	if err := json.Unmarshal(s.gotConfig, &got); err != nil {
		t.Errorf("Get gotConfig Unmarshal error = %v", err)
	}

	if want := "sample_another_config"; got != want {
		t.Errorf("Get config = %v, want %v", got, want)
	}
}
