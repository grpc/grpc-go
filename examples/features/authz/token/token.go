/*
 *
 * Copyright 2023 gRPC authors.
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

package token

import (
	"encoding/base64"
	"encoding/json"
)

// Token is exchanges between client and server for user authorization against a policy
type Token struct {
	// Secret is used by the server to authenticate the user
	Secret string `json:"secret"`
	// Username is used by the server to assign roles in the metadata for authorization
	Username string `json:"username"`
}

// Encode returns a base64 string of the token json representation
func (t *Token) Encode() (string, error) {
	barr, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	s := base64.StdEncoding.EncodeToString(barr)
	return s, nil
}

// Decode sets a token from a base64 encoded string of the token json representation
func (t *Token) Decode(s string) error {
	barr, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(barr, t)
}
