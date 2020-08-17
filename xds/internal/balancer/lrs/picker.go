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

package lrs

import (
	orcapb "github.com/cncf/udpa/go/udpa/data/orca/v1"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/xds/internal"
)

const (
	serverLoadCPUName    = "cpu_utilization"
	serverLoadMemoryName = "mem_utilization"
)

type loadReportPicker struct {
	p balancer.Picker

	id        internal.LocalityID
	loadStore Store
}

func newLoadReportPicker(p balancer.Picker, id internal.LocalityID, loadStore Store) *loadReportPicker {
	return &loadReportPicker{
		p:         p,
		id:        id,
		loadStore: loadStore,
	}
}

func (lrp *loadReportPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	res, err := lrp.p.Pick(info)
	if err != nil {
		return res, err
	}

	lrp.loadStore.CallStarted(lrp.id)
	oldDone := res.Done
	res.Done = func(info balancer.DoneInfo) {
		if oldDone != nil {
			oldDone(info)
		}
		lrp.loadStore.CallFinished(lrp.id, info.Err)

		load, ok := info.ServerLoad.(*orcapb.OrcaLoadReport)
		if !ok {
			return
		}
		lrp.loadStore.CallServerLoad(lrp.id, serverLoadCPUName, load.CpuUtilization)
		lrp.loadStore.CallServerLoad(lrp.id, serverLoadMemoryName, load.MemUtilization)
		for n, d := range load.RequestCost {
			lrp.loadStore.CallServerLoad(lrp.id, n, d)
		}
		for n, d := range load.Utilization {
			lrp.loadStore.CallServerLoad(lrp.id, n, d)
		}
	}
	return res, err
}
