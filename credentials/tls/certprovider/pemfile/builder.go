/*
 *
 * Copyright 2020 gRPC authors.
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

package pemfile

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"google.golang.org/grpc/credentials/tls/certprovider"
)

const (
	// PluginName is the name of the PEM file watcher plugin.
	PluginName             = "file_watcher"
	defaultRefreshInterval = 10 * time.Minute
)

func init() {
	certprovider.Register(&pluginBuilder{})
}

type pluginBuilder struct{}

func (p *pluginBuilder) ParseConfig(c any) (*certprovider.BuildableConfig, error) {
	data, ok := c.(json.RawMessage)
	if !ok {
		return nil, fmt.Errorf("meshca: unsupported config type: %T", c)
	}
	opts, err := pluginConfigFromJSON(data)
	if err != nil {
		return nil, err
	}
	return certprovider.NewBuildableConfig(PluginName, opts.canonical(), func(certprovider.BuildOptions) certprovider.Provider {
		return newProvider(opts)
	}), nil
}

func (p *pluginBuilder) Name() string {
	return PluginName
}

func pluginConfigFromJSON(jd json.RawMessage) (Options, error) {
	// The only difference between this anonymous struct and the Options struct
	// is that the refresh_interval is represented here as a duration proto,
	// while in the latter a time.Duration is used.
	opts := Options{
		RefreshDuration: defaultRefreshInterval,
	}

	if err := json.Unmarshal(jd, &opts); err != nil {
		return Options{}, fmt.Errorf("pemfile: json.Unmarshal(%s) failed: %v", string(jd), err)
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(jd, &rawMap); err != nil {
		return Options{}, fmt.Errorf("pemfile: json.Unmarshal map failed: %v", err)
	}

	if rawMap["refresh_interval"] != nil {
		dur := &durationpb.Duration{}
		if err := protojson.Unmarshal(rawMap["refresh_interval"], dur); err != nil {
			return Options{}, fmt.Errorf("pemfile: protojson.Unmarshal(%+v) failed: %v", rawMap["refresh_interval"], err)
		}
		opts.RefreshDuration = dur.AsDuration()
	}

	if err := opts.validate(); err != nil {
		return Options{}, err
	}
	return opts, nil
}
