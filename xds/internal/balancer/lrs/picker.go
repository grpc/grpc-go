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
)

const (
	serverLoadCPUName    = "cpu_utilization"
	serverLoadMemoryName = "mem_utilization"
)

// loadReporter wraps the methods from the loadStore that are used here.
type loadReporter interface {
	CallStarted(locality string)
	CallFinished(locality string, err error)
	CallServerLoad(locality, name string, val float64)
}

type loadReportPicker struct {
	p balancer.Picker

	locality  string
	loadStore loadReporter
}

func newLoadReportPicker(p balancer.Picker, id string, loadStore loadReporter) *loadReportPicker {
	return &loadReportPicker{
		p:         p,
		locality:  id,
		loadStore: loadStore,
	}
}

func (lrp *loadReportPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	res, err := lrp.p.Pick(info)
	if err != nil {
		return res, err
	}

	if lrp.loadStore == nil {
		return res, err
	}

	lrp.loadStore.CallStarted(lrp.locality)
	oldDone := res.Done
	res.Done = func(info balancer.DoneInfo) {
		if oldDone != nil {
			oldDone(info)
		}
		lrp.loadStore.CallFinished(lrp.locality, info.Err)

		load, ok := info.ServerLoad.(*orcapb.OrcaLoadReport)
		if !ok {
			return
		}
		lrp.loadStore.CallServerLoad(lrp.locality, serverLoadCPUName, load.CpuUtilization)
		lrp.loadStore.CallServerLoad(lrp.locality, serverLoadMemoryName, load.MemUtilization)
		for n, d := range load.RequestCost {
			lrp.loadStore.CallServerLoad(lrp.locality, n, d)
		}
		for n, d := range load.Utilization {
			lrp.loadStore.CallServerLoad(lrp.locality, n, d)
		}
	}
	return res, err
}
