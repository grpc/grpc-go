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
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/grpclog"
)

// tLogr serves as the grpclog logger and is the interface through which
// expected errors are declared in tests.
var tLogr *tLogger

const callingFrame = 4

type logType int

func (l logType) String() string {
	switch l {
	case infoLog:
		return "INFO"
	case warningLog:
		return "WARNING"
	case errorLog:
		return "ERROR"
	case fatalLog:
		return "FATAL"
	}
	return "UNKNOWN"
}

const (
	infoLog logType = iota
	warningLog
	errorLog
	fatalLog
)

type tLogger struct {
	v           int
	initialized bool

	mu     sync.Mutex // guards t, start, and errors
	t      *testing.T
	start  time.Time
	errors map[*regexp.Regexp]int
}

func init() {
	vLevel := 0 // Default verbosity level

	if vLevelEnv, found := os.LookupEnv("GRPC_GO_LOG_VERBOSITY_LEVEL"); found {
		// If found, attempt to convert. If conversion is successful, update vLevel.
		// If conversion fails, log a warning, but vLevel remains its default of 0.
		if val, err := strconv.Atoi(vLevelEnv); err == nil {
			vLevel = val
		} else {
			// Log the error if the environment variable is not a valid integer.
			fmt.Printf("Warning: GRPC_GO_LOG_VERBOSITY_LEVEL environment variable '%s' is not a valid integer. "+
				"Using default verbosity level 0. Error: %v\n", vLevelEnv, err)
		}
	}
	// Initialize tLogr with the determined verbosity level.
	tLogr = &tLogger{errors: make(map[*regexp.Regexp]int), v: vLevel}
}

// getCallingPrefix returns the <file:line> at the given depth from the stack.
func getCallingPrefix(depth int) (string, error) {
	_, file, line, ok := runtime.Caller(depth)
	if !ok {
		return "", errors.New("frame request out-of-bounds")
	}
	return fmt.Sprintf("%s:%d", path.Base(file), line), nil
}

// log logs the message with the specified parameters to the tLogger.
func (tl *tLogger) log(ltype logType, depth int, format string, args ...any) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	prefix, err := getCallingPrefix(callingFrame + depth)
	if err != nil {
		tl.t.Error(err)
		return
	}
	args = append([]any{ltype.String() + " " + prefix}, args...)
	args = append(args, fmt.Sprintf(" (t=+%s)", time.Since(tl.start)))

	if format == "" {
		switch ltype {
		case errorLog:
			// fmt.Sprintln is used rather than fmt.Sprint because tl.Log uses fmt.Sprintln behavior.
			if tl.expected(fmt.Sprintln(args...)) {
				tl.t.Log(args...)
			} else {
				tl.t.Error(args...)
			}
		case fatalLog:
			panic(fmt.Sprint(args...))
		default:
			tl.t.Log(args...)
		}
	} else {
		// Add formatting directives for the callingPrefix and timeSuffix.
		format = "%v " + format + "%s"
		switch ltype {
		case errorLog:
			if tl.expected(fmt.Sprintf(format, args...)) {
				tl.t.Logf(format, args...)
			} else {
				tl.t.Errorf(format, args...)
			}
		case fatalLog:
			panic(fmt.Sprintf(format, args...))
		default:
			tl.t.Logf(format, args...)
		}
	}
}

// update updates the testing.T that the testing logger logs to. Should be done
// before every test. It also initializes the tLogger if it has not already.
func (tl *tLogger) update(t *testing.T) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if !tl.initialized {
		grpclog.SetLoggerV2(tl)
		tl.initialized = true
	}
	tl.t = t
	tl.start = time.Now()
	tl.errors = map[*regexp.Regexp]int{}
}

// ExpectError declares an error to be expected. For the next test, the first
// error log matching the expression (using FindString) will not cause the test
// to fail. "For the next test" includes all the time until the next call to
// Update(). Note that if an expected error is not encountered, this will cause
// the test to fail.
func ExpectError(expr string) {
	ExpectErrorN(expr, 1)
}

// ExpectErrorN declares an error to be expected n times.
func ExpectErrorN(expr string, n int) {
	tLogr.mu.Lock()
	defer tLogr.mu.Unlock()
	re, err := regexp.Compile(expr)
	if err != nil {
		tLogr.t.Error(err)
		return
	}
	tLogr.errors[re] += n
}

// endTest checks if expected errors were not encountered.
func (tl *tLogger) endTest(t *testing.T) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	for re, count := range tl.errors {
		if count > 0 {
			t.Errorf("Expected error '%v' not encountered", re.String())
		}
	}
	tl.errors = map[*regexp.Regexp]int{}
}

// expected determines if the error string is protected or not.
func (tl *tLogger) expected(s string) bool {
	for re, count := range tl.errors {
		if re.FindStringIndex(s) != nil {
			tl.errors[re]--
			if count <= 1 {
				delete(tl.errors, re)
			}
			return true
		}
	}
	return false
}

func (tl *tLogger) Info(args ...any) {
	tl.log(infoLog, 0, "", args...)
}

func (tl *tLogger) Infoln(args ...any) {
	tl.log(infoLog, 0, "", args...)
}

func (tl *tLogger) Infof(format string, args ...any) {
	tl.log(infoLog, 0, format, args...)
}

func (tl *tLogger) InfoDepth(depth int, args ...any) {
	tl.log(infoLog, depth, "", args...)
}

func (tl *tLogger) Warning(args ...any) {
	tl.log(warningLog, 0, "", args...)
}

func (tl *tLogger) Warningln(args ...any) {
	tl.log(warningLog, 0, "", args...)
}

func (tl *tLogger) Warningf(format string, args ...any) {
	tl.log(warningLog, 0, format, args...)
}

func (tl *tLogger) WarningDepth(depth int, args ...any) {
	tl.log(warningLog, depth, "", args...)
}

func (tl *tLogger) Error(args ...any) {
	tl.log(errorLog, 0, "", args...)
}

func (tl *tLogger) Errorln(args ...any) {
	tl.log(errorLog, 0, "", args...)
}

func (tl *tLogger) Errorf(format string, args ...any) {
	tl.log(errorLog, 0, format, args...)
}

func (tl *tLogger) ErrorDepth(depth int, args ...any) {
	tl.log(errorLog, depth, "", args...)
}

func (tl *tLogger) Fatal(args ...any) {
	tl.log(fatalLog, 0, "", args...)
}

func (tl *tLogger) Fatalln(args ...any) {
	tl.log(fatalLog, 0, "", args...)
}

func (tl *tLogger) Fatalf(format string, args ...any) {
	tl.log(fatalLog, 0, format, args...)
}

func (tl *tLogger) FatalDepth(depth int, args ...any) {
	tl.log(fatalLog, depth, "", args...)
}

func (tl *tLogger) V(l int) bool {
	return l <= tl.v
}
