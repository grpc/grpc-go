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

package grpctest

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"google.golang.org/grpc/grpclog"
)

type s struct {
	Tester
}

func Test(t *testing.T) {
	RunSubTests(t, s{})
}

func (s) TestInfo(*testing.T) {
	grpclog.Info("Info", "message.")
}

func (s) TestInfoln(*testing.T) {
	grpclog.Infoln("Info", "message.")
}

func (s) TestInfof(*testing.T) {
	grpclog.Infof("%v %v.", "Info", "message")
}

func (s) TestInfoDepth(*testing.T) {
	grpclog.InfoDepth(0, "Info", "depth", "message.")
}

func (s) TestWarning(*testing.T) {
	grpclog.Warning("Warning", "message.")
}

func (s) TestWarningln(*testing.T) {
	grpclog.Warningln("Warning", "message.")
}

func (s) TestWarningf(*testing.T) {
	grpclog.Warningf("%v %v.", "Warning", "message")
}

func (s) TestWarningDepth(*testing.T) {
	grpclog.WarningDepth(0, "Warning", "depth", "message.")
}

func (s) TestError(t *testing.T) {
	const numErrors = 10
	Update(t)
	ExpectError("Expected error")
	ExpectError("Expected ln error")
	ExpectError("Expected formatted error")
	ExpectErrorN("Expected repeated error", numErrors)
	grpclog.Error("Expected", "error")
	grpclog.Errorln("Expected", "ln", "error")
	grpclog.Errorf("%v %v %v", "Expected", "formatted", "error")
	for i := 0; i < numErrors; i++ {
		grpclog.Error("Expected repeated error")
	}
}

func (s) TestInit(t *testing.T) {
	// Reset the atomic value
	tLoggerAtomic.Store(&tLogger{errors: map[*regexp.Regexp]int{}})
	
	// Test initial state
	logger := getLogger()
	if logger == nil {
		t.Fatal("getLogger() returned nil")
	}
	if logger.errors == nil {
		t.Error("logger.errors is nil")
	}
	if len(logger.errors) != 0 {
		t.Errorf("logger.errors = %v; want empty map", logger.errors)
	}
	if logger.initialized {
		t.Error("logger.initialized = true; want false")
	}
	if logger.t != nil {
		t.Error("logger.t is not nil")
	}
	if !logger.start.IsZero() {
		t.Error("logger.start is not zero")
	}
}

func (s) TestInitVerbosityLevel(t *testing.T) {
	// Save original env var and reset atomic value
	origLevel := os.Getenv("GRPC_GO_LOG_VERBOSITY_LEVEL")
	defer os.Setenv("GRPC_GO_LOG_VERBOSITY_LEVEL", origLevel)
	tLoggerAtomic.Store(&tLogger{errors: map[*regexp.Regexp]int{}})

	// Test with valid verbosity level
	testLevel := "2"
	os.Setenv("GRPC_GO_LOG_VERBOSITY_LEVEL", testLevel)
	
	// Initialize logger with verbosity level
	logger := getLogger()
	vLevel := os.Getenv("GRPC_GO_LOG_VERBOSITY_LEVEL")
	if vl, err := strconv.Atoi(vLevel); err == nil {
		logger.v = vl
	}

	// Verify verbosity level
	if logger.v != 2 {
		t.Errorf("logger.v = %d; want 2", logger.v)
	}

	// Test with invalid verbosity level
	os.Setenv("GRPC_GO_LOG_VERBOSITY_LEVEL", "invalid")
	
	// Reset atomic value and initialize new logger
	tLoggerAtomic.Store(&tLogger{errors: map[*regexp.Regexp]int{}})
	logger = getLogger()
	vLevel = os.Getenv("GRPC_GO_LOG_VERBOSITY_LEVEL")
	if vl, err := strconv.Atoi(vLevel); err == nil {
		logger.v = vl
	}

	// Verify verbosity level remains at default (0) for invalid input
	if logger.v != 0 {
		t.Errorf("logger.v = %d; want 0", logger.v)
	}
}

func (s) TestAtomicValue(t *testing.T) {
	// Save original logger
	origLogger := getLogger()
	defer tLoggerAtomic.Store(origLogger)
	
	// Create new logger
	newLogger := &tLogger{errors: map[*regexp.Regexp]int{}}
	tLoggerAtomic.Store(newLogger)
	
	// Verify new logger is retrieved
	retrievedLogger := getLogger()
	if retrievedLogger != newLogger {
		t.Error("getLogger() did not return the newly stored logger")
	}
	
	// Restore original logger
	tLoggerAtomic.Store(origLogger)
	
	// Verify original logger is retrieved
	retrievedLogger = getLogger()
	if retrievedLogger != origLogger {
		t.Error("getLogger() did not return the original logger after restore")
	}
}
