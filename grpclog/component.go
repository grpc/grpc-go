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

package grpclog

import (
	"fmt"
	"sync"
)

// componentData records the settings for a component.
type componentData struct {
	name string
}

// cache stores previously created component loggers. It uses a sync.Map to
// allow safe concurrent access from multiple goroutines without contention.
// A sync.Map is used because, while static packages initialize component loggers
// at package init() time, dynamic subsystems (such as xDS client streams and
// authorities in internal/xds/clients/... as well as custom resolvers and
// balancers) call Component() at runtime from concurrent goroutines, which
// would cause data races and concurrent map read/write panics on a plain map.
var cache sync.Map // map[string]*componentData

func (c *componentData) InfoDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	InfoDepth(depth+1, args...)
}

func (c *componentData) WarningDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	WarningDepth(depth+1, args...)
}

func (c *componentData) ErrorDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	ErrorDepth(depth+1, args...)
}

func (c *componentData) FatalDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	FatalDepth(depth+1, args...)
}

func (c *componentData) Info(args ...any) {
	c.InfoDepth(1, args...)
}

func (c *componentData) Warning(args ...any) {
	c.WarningDepth(1, args...)
}

func (c *componentData) Error(args ...any) {
	c.ErrorDepth(1, args...)
}

func (c *componentData) Fatal(args ...any) {
	c.FatalDepth(1, args...)
}

func (c *componentData) Infof(format string, args ...any) {
	c.InfoDepth(1, fmt.Sprintf(format, args...))
}

func (c *componentData) Warningf(format string, args ...any) {
	c.WarningDepth(1, fmt.Sprintf(format, args...))
}

func (c *componentData) Errorf(format string, args ...any) {
	c.ErrorDepth(1, fmt.Sprintf(format, args...))
}

func (c *componentData) Fatalf(format string, args ...any) {
	c.FatalDepth(1, fmt.Sprintf(format, args...))
}

func (c *componentData) Infoln(args ...any) {
	c.InfoDepth(1, args...)
}

func (c *componentData) Warningln(args ...any) {
	c.WarningDepth(1, args...)
}

func (c *componentData) Errorln(args ...any) {
	c.ErrorDepth(1, args...)
}

func (c *componentData) Fatalln(args ...any) {
	c.FatalDepth(1, args...)
}

func (c *componentData) V(l int) bool {
	return V(l)
}

// Component creates a new component and returns it for logging. If a component
// with the name already exists, nothing will be created and it will be
// returned. SetLoggerV2 will panic if it is called with a logger created by
// Component.
func Component(componentName string) DepthLoggerV2 {
	if cData, ok := cache.Load(componentName); ok {
		return cData.(*componentData)
	}
	c := &componentData{componentName}
	cData, _ := cache.LoadOrStore(componentName, c)
	return cData.(*componentData)
}
