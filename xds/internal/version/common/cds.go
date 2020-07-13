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

package common

import (
	"fmt"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/internal/grpclog"
)

// UnmarshalCluster processes resources received in an CDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalCluster(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ClusterUpdate, error) {
	update := make(map[string]ClusterUpdate)
	for _, r := range resources {
		if t := r.GetTypeUrl(); t != V2ClusterURL && t != V3ClusterURL {
			return nil, fmt.Errorf("xds: unexpected resource type: %s in CDS response", t)
		}

		cluster := &v3clusterpb.Cluster{}
		if err := proto.Unmarshal(r.GetValue(), cluster); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in CDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", cluster.GetName(), cluster, cluster)
		cu, err := validateCluster(cluster)
		if err != nil {
			return nil, err
		}

		// If the Cluster message in the CDS response did not contain a
		// serviceName, we will just use the clusterName for EDS.
		if cu.ServiceName == "" {
			cu.ServiceName = cluster.GetName()
		}
		logger.Debugf("Resource with name %v, value %+v added to cache", cluster.GetName(), cu)
		update[cluster.GetName()] = cu
	}
	return update, nil
}

func validateCluster(cluster *v3clusterpb.Cluster) (ClusterUpdate, error) {
	emptyUpdate := ClusterUpdate{ServiceName: "", EnableLRS: false}
	switch {
	case cluster.GetType() != v3clusterpb.Cluster_EDS:
		return emptyUpdate, fmt.Errorf("xds: unexpected cluster type %v in response: %+v", cluster.GetType(), cluster)
	case cluster.GetEdsClusterConfig().GetEdsConfig().GetAds() == nil:
		return emptyUpdate, fmt.Errorf("xds: unexpected edsConfig in response: %+v", cluster)
	case cluster.GetLbPolicy() != v3clusterpb.Cluster_ROUND_ROBIN:
		return emptyUpdate, fmt.Errorf("xds: unexpected lbPolicy %v in response: %+v", cluster.GetLbPolicy(), cluster)
	}

	return ClusterUpdate{
		ServiceName: cluster.GetEdsClusterConfig().GetServiceName(),
		EnableLRS:   cluster.GetLrsServer().GetSelf() != nil,
	}, nil
}
