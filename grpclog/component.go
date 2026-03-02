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

	infoDepth    func(depth int, args ...any)
	warningDepth func(depth int, args ...any)
	errorDepth   func(depth int, args ...any)
	fatalDepth   func(depth int, args ...any)
}

type componentCache struct {
	mu   sync.Mutex
	data map[string]*componentData
}

var cache = componentCache{
	data: map[string]*componentData{},
}

func (c *componentData) InfoDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	c.infoDepth(depth+1, args...)
}

func (c *componentData) WarningDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	c.warningDepth(depth+1, args...)
}

func (c *componentData) ErrorDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	c.errorDepth(depth+1, args...)
}

func (c *componentData) FatalDepth(depth int, args ...any) {
	args = append([]any{"[" + string(c.name) + "]"}, args...)
	c.fatalDepth(depth+1, args...)
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

func noopDepth(int, ...any) {
}

// Component creates a new component and returns it for logging. If a component
// with the name already exists, nothing will be created and it will be
// returned. SetLoggerV2 will panic if it is called with a logger created by
// Component.
func Component(componentName string) DepthLoggerV2 {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cData, ok := cache.data[componentName]; ok {
		return cData
	}
	c := &componentData{
		name:         componentName,
		infoDepth:    InfoDepth,
		warningDepth: WarningDepth,
		errorDepth:   ErrorDepth,
		fatalDepth:   FatalDepth,
	}

	if level, ok := componentLogLevels[componentName]; ok {
		c.fatalDepth = func(depth int, args ...any) {
			componentFatalDepth(depth+1, args...)
		}
		switch level {
		case severityInfo:
			c.infoDepth = func(depth int, args ...any) {
				componentInfoDepth(depth+1, args...)
			}
			c.warningDepth = func(depth int, args ...any) {
				componentWarningDepth(depth+1, args...)
			}
			c.errorDepth = func(depth int, args ...any) {
				componentErrorDepth(depth+1, args...)
			}
		case severityWarning:
			c.infoDepth = noopDepth
			c.warningDepth = func(depth int, args ...any) {
				componentWarningDepth(depth+1, args...)
			}
			c.errorDepth = func(depth int, args ...any) {
				componentErrorDepth(depth+1, args...)
			}
		case severityError:
			c.infoDepth = noopDepth
			c.warningDepth = noopDepth
			c.errorDepth = func(depth int, args ...any) {
				componentErrorDepth(depth+1, args...)
			}
		}
	}

	cache.data[componentName] = c
	return c
}
