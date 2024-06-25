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

import "log"

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

// DefaultNonPerCallMetrics are the instruments registered that are on by
// default.
var DefaultNonPerCallMetrics = make(map[string]bool)

// ClearInstrumentRegistryForTesting clears the instrument registry for testing
// purposes only.
func ClearInstrumentRegistryForTesting() {
	Int64CountInsts = nil
	Float64CountInsts = nil
	Int64HistoInsts = nil
	Float64HistoInsts = nil
	Int64GaugeInsts = nil
	registeredInsts = make(map[string]bool)
	DefaultNonPerCallMetrics = make(map[string]bool)
}

// Label represents a string attribute/label to attach to metrics.
type Label struct {
	// Key is the key of the label.
	Key string
	// Value is the value of the label.
	Value string
}

func registerInst(name string, def bool) {
	if registeredInsts[name] {
		log.Panicf("instrument %v already registered", name)
	}
	registeredInsts[name] = true
	if def {
		DefaultNonPerCallMetrics[name] = true
	}
}

// Int64CountHandle is a typed handle for a int count instrument. This handle is
// passed at the recording point in order to know which instrument to record on.
type Int64CountHandle struct {
	Index int
}

// RegisterInt64Count registers the int count instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Count(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) Int64CountHandle {
	registerInst(name, def)
	Int64CountInsts = append(Int64CountInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return Int64CountHandle{
		Index: len(Int64CountInsts) - 1,
	}
}

// Float64CountHandle is a typed handle for a float count instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Float64CountHandle struct {
	Index int
}

// RegisterFloat64Count registers the float count instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Count(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) Float64CountHandle {
	registerInst(name, def)
	Float64CountInsts = append(Float64CountInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return Float64CountHandle{
		Index: len(Float64CountInsts) - 1,
	}
}

// Int64HistoHandle is a typed handle for a int histogram instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Int64HistoHandle struct {
	Index int
}

// RegisterInt64Histo registers the int histogram instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Histo(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) Int64HistoHandle {
	registerInst(name, def)
	Int64HistoInsts = append(Int64HistoInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return Int64HistoHandle{
		Index: len(Int64HistoInsts) - 1,
	}
}

// Float64HistoHandle is a typed handle for a float histogram instrument. This
// handle is passed at the recording point in order to know which instrument to
// record on.
type Float64HistoHandle struct {
	Index int
}

// RegisterFloat64Histo registers the float histogram instrument description
// onto the global registry. It returns a typed handle to use when recording
// data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Histo(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) Float64HistoHandle {
	registerInst(name, def)
	Float64HistoInsts = append(Float64HistoInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return Float64HistoHandle{
		Index: len(Float64HistoInsts) - 1,
	}
}

// Int64GaugeHandle is a typed handle for a int gauge instrument. This handle is
// passed at the recording point in order to know which instrument to record on.
type Int64GaugeHandle struct {
	Index int
}

// RegisterInt64Gauge registers the int gauge instrument description onto the
// global registry. It returns a typed handle to use when recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Gauge(name string, desc string, unit string, labels []string, optionalLabels []string, def bool) Int64GaugeHandle {
	registerInst(name, def)
	Int64GaugeInsts = append(Int64GaugeInsts, InstrumentDescriptor{
		Name:           name,
		Description:    desc,
		Unit:           unit,
		Labels:         labels,
		OptionalLabels: optionalLabels,
		Default:        def,
	})
	return Int64GaugeHandle{
		Index: len(Int64GaugeInsts) - 1,
	}
}
