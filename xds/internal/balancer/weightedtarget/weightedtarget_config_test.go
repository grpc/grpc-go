// +build go1.12

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

package weightedtarget

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"

	_ "google.golang.org/grpc/xds/internal/balancer/lrs" // Register LRS balancer, so we can use it as child policy in the config tests.
)

const (
	testJSONConfig = `{
  "targets": {
	"cluster_1" : {
	  "weight":75,
	  "childPolicy":[{"lrs_experimental":{"clusterName":"cluster_1","lrsLoadReportingServerName":"lrs.server","locality":{"zone":"test-zone-1"}}}]
	},
	"cluster_2" : {
	  "weight":25,
	  "childPolicy":[{"lrs_experimental":{"clusterName":"cluster_2","lrsLoadReportingServerName":"lrs.server","locality":{"zone":"test-zone-2"}}}]
	}
  }
}`
	lrsBalancerName = "lrs_experimental"
)

var (
	lrsConfigParser = balancer.Get(lrsBalancerName).(balancer.ConfigParser)
	lrsConfigJSON1  = `{"clusterName":"cluster_1","lrsLoadReportingServerName":"lrs.server","locality":{"zone":"test-zone-1"}}`
	lrsConfig1, _   = lrsConfigParser.ParseConfig([]byte(lrsConfigJSON1))
	lrsConfigJSON2  = `{"clusterName":"cluster_2","lrsLoadReportingServerName":"lrs.server","locality":{"zone":"test-zone-2"}}`
	lrsConfig2, _   = lrsConfigParser.ParseConfig([]byte(lrsConfigJSON2))
)

func Test_parseConfig(t *testing.T) {
	tests := []struct {
		name    string
		js      string
		want    *LBConfig
		wantErr bool
	}{
		{
			name:    "empty json",
			js:      "",
			want:    nil,
			wantErr: true,
		},
		{
			name: "OK",
			js:   testJSONConfig,
			want: &LBConfig{
				Targets: map[string]Target{
					"cluster_1": {
						Weight: 75,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name:   lrsBalancerName,
							Config: lrsConfig1,
						},
					},
					"cluster_2": {
						Weight: 25,
						ChildPolicy: &internalserviceconfig.BalancerConfig{
							Name:   lrsBalancerName,
							Config: lrsConfig2,
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig([]byte(tt.js))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf("parseConfig() got unexpected result, diff: %v", cmp.Diff(got, tt.want))
			}
		})
	}
}
