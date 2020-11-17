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
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	pluginName             = "file_watcher"
	defaultRefreshInterval = 10 * time.Minute
)

func init() {
	certprovider.Register(&pluginBuilder{})
}

type pluginBuilder struct{}

func (p *pluginBuilder) ParseConfig(c interface{}) (*certprovider.BuildableConfig, error) {
	data, ok := c.(json.RawMessage)
	if !ok {
		return nil, fmt.Errorf("meshca: unsupported config type: %T", c)
	}
	cfg, err := pluginConfigFromJSON(data)
	if err != nil {
		return nil, err
	}
	return certprovider.NewBuildableConfig(pluginName, cfg.canonical(), func(certprovider.BuildOptions) certprovider.Provider {
		return newProvider(Options{
			CertFile:        cfg.certFile,
			KeyFile:         cfg.keyFile,
			RootFile:        cfg.keyFile,
			RefreshDuration: cfg.refreshInterval,
		})
	}), nil
}

func (p *pluginBuilder) Name() string {
	return pluginName
}

type pluginConfig struct {
	certFile        string
	keyFile         string
	caFile          string
	refreshInterval time.Duration
}

func (pc *pluginConfig) canonical() []byte {
	return []byte(fmt.Sprintf("%s:%s:%s:%s", pc.certFile, pc.keyFile, pc.caFile, pc.refreshInterval))
}

func pluginConfigFromJSON(jd json.RawMessage) (*pluginConfig, error) {
	// Anonymous struct to unmarshal the JSON config into.
	cfg := &struct {
		CertificateFile   string          `json:"certificate_file,omitempty"`
		PrivateKeyFile    string          `json:"private_key_file,omitempty"`
		CACertificateFile string          `json:"ca_certificate_file,omitempty"`
		RefreshInterval   json.RawMessage `json:"refresh_interval,omitempty"`
	}{}
	if err := json.Unmarshal(jd, cfg); err != nil {
		return nil, fmt.Errorf("pemfile: json.Unmarshal(%s) failed: %v", string(jd), err)
	}

	pc := &pluginConfig{
		certFile: cfg.CertificateFile,
		keyFile:  cfg.PrivateKeyFile,
		caFile:   cfg.CACertificateFile,
		// Refresh interval is the only field in the configuration for which we
		// support a default value. We cannot possibly have valid defaults for
		// file paths to watch. Also, it is valid to specify an empty path for
		// some of those fields if the user does not want to watch them.
		refreshInterval: defaultRefreshInterval,
	}
	if cfg.RefreshInterval != nil {
		dur := &durationpb.Duration{}
		if err := protojson.Unmarshal(cfg.RefreshInterval, dur); err != nil {
			return nil, fmt.Errorf("pemfile: protojson.Unmarshal(%+v) failed: %v", cfg.RefreshInterval, err)
		}
		pc.refreshInterval = dur.AsDuration()
	}

	if pc.certFile == "" && pc.keyFile == "" && pc.caFile == "" {
		return nil, fmt.Errorf("pemfile: at least one credential file needs to be specified")
	}
	if keySpecified, certSpecified := pc.keyFile != "", pc.certFile != ""; keySpecified != certSpecified {
		return nil, fmt.Errorf("pemfile: private key file and identity cert file should be both specified or not specified")
	}
	// C-core has a limitation that they cannot verify that a certificate file
	// matches a key file. So, the only way to get around this is to make sure
	// that both files are in the same directory and that they do an atomic
	// read. Even though Java/Go do not have this limitation, we want the
	// overall plugin behavior to be consistent across languages.
	if certDir, keyDir := filepath.Dir(pc.certFile), filepath.Dir(pc.keyFile); certDir != keyDir {
		return nil, errors.New("pemfile: certificate and key file must be in the same directory")
	}
	return pc, nil
}
