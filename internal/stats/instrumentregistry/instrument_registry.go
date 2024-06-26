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

// Package instrumentregistry is a instrument registry that gRPC components can
// use to register instruments (metrics).
package instrumentregistry

import (
	"log"

	"google.golang.org/grpc/experimental/stats/instrumentregistry"
	"google.golang.org/grpc/stats"
)

// InstrumentDescriptor is a data of a registered instrument (metric).
type InstrumentDescriptor struct {
	// Name is the name of this metric.
	Name string
	// Description is the description of this metric.
	Description string
	// Unit is the unit of this metric.
	Unit string
	// Labels are the required label keys for this metric.
	Labels []string
	// OptionalLabels are the optional label keys for this
	// metric.
	OptionalLabels []string
	// Default is whether this metric is on by default.
	Default bool
}

// All of these globals are written to at initialization time only, and are read
// only after initialization.

// Int64CountInsts is information about registered int 64 count instruments in
// order of registration.
var Int64CountInsts []InstrumentDescriptor

// Float64CountInsts is information about registered float 64 count instruments
// in order of registration.
var Float64CountInsts []InstrumentDescriptor

// Int64HistoInsts is information about registered int 64 histo instruments in
// order of registration.
var Int64HistoInsts []InstrumentDescriptor

// Float64HistoInsts is information about registered float 64 histo instruments
// in order of registration.
var Float64HistoInsts []InstrumentDescriptor

// Int64GaugeInsts is information about registered int 64 gauge instruments in
// order of registration.
var Int64GaugeInsts []InstrumentDescriptor

// registeredInsts are the registered instrument descriptor names.
var registeredInsts = make(map[string]bool)

// ClearInstrumentRegistryForTesting clears the instrument registry for testing
// purposes only.
func ClearInstrumentRegistryForTesting() {
	Int64CountInsts = nil
	Float64CountInsts = nil
	Int64HistoInsts = nil
	Float64HistoInsts = nil
	Int64GaugeInsts = nil
	registeredInsts = make(map[string]bool)
	instrumentregistry.DefaultMetrics = make(map[stats.Metric]bool)
}

func registerInst(name string, def bool) {
	if registeredInsts[name] {
		log.Panicf("instrument %v already registered", name)
	}
	registeredInsts[name] = true
	if def {
		instrumentregistry.DefaultMetrics[stats.Metric(name)] = true
	}
}

// RegisterInt64Count registers the int count instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Count(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) instrumentregistry.Int64CountHandle {
	registerInst(name, def)
	Int64CountInsts = append(Int64CountInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return instrumentregistry.Int64CountHandle{
		Index: len(Int64CountInsts) - 1,
	}
}

// RegisterFloat64Count registers the float count instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Count(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) instrumentregistry.Float64CountHandle {
	registerInst(name, def)
	Float64CountInsts = append(Float64CountInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return instrumentregistry.Float64CountHandle{
		Index: len(Float64CountInsts) - 1,
	}
}

// RegisterInt64Histo registers the int histogram instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Histo(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) instrumentregistry.Int64HistoHandle {
	registerInst(name, def)
	Int64HistoInsts = append(Int64HistoInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return instrumentregistry.Int64HistoHandle{
		Index: len(Int64HistoInsts) - 1,
	}
}

// RegisterFloat64Histo registers the float histogram instrument description
// onto the global registry. It returns a typed handle to use when recording
// data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Histo(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) instrumentregistry.Float64HistoHandle {
	registerInst(name, def)
	Float64HistoInsts = append(Float64HistoInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return instrumentregistry.Float64HistoHandle{
		Index: len(Float64HistoInsts) - 1,
	}
}

// RegisterInt64Gauge registers the int gauge instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Gauge(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) instrumentregistry.Int64GaugeHandle {
	registerInst(name, def)
	Int64GaugeInsts = append(Int64GaugeInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return instrumentregistry.Int64GaugeHandle{
		Index: len(Int64GaugeInsts) - 1,
	}
}
