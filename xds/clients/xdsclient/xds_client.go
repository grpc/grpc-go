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

package xdsclient

// XDSClient represents an xDS client.
type XDSClient struct {
	config *Config
}

// New returns an xDS Client configured with provided config.
func New(config Config) (*XDSClient, error) {
	return &XDSClient{config: &config}, nil
}

// WatchResource uses xDS to discover the resource associated
// with the provided resource name. The resource type implementation
// determines how xDS responses are received, are deserialized
// and validated. Upon receipt of a response from the management
// server, an appropriate callback on the watcher is invoked.
func (c *XDSClient) WatchResource(rType ResourceType, resourceName string, watcher ResourceWatcher) (cancel func()) {
	return func() {}
}

// Close closes the xDS client and releases all resources.
// The caller is expected to invoke it once they are done
// using the client
func Close() error {
	return nil
}
