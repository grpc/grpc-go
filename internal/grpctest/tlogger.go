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

// TLogger serves as the grpclog logger and is the interface through which
// expected errors are declared in tests.
var TLogger *tLogger

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
	TLogger = &tLogger{errors: map[*regexp.Regexp]int{}}
	vLevel := os.Getenv("GRPC_GO_LOG_VERBOSITY_LEVEL")
	if vl, err := strconv.Atoi(vLevel); err == nil {
		TLogger.v = vl
	}
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
func (t *tLogger) log(ltype logType, depth int, format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prefix, err := getCallingPrefix(callingFrame + depth)
	if err != nil {
		t.t.Error(err)
		return
	}
	args = append([]any{ltype.String() + " " + prefix}, args...)
	args = append(args, fmt.Sprintf(" (t=+%s)", time.Since(t.start)))

	if format == "" {
		switch ltype {
		case errorLog:
			// fmt.Sprintln is used rather than fmt.Sprint because t.Log uses fmt.Sprintln behavior.
			if t.expected(fmt.Sprintln(args...)) {
				t.t.Log(args...)
			} else {
				t.t.Error(args...)
			}
		case fatalLog:
			panic(fmt.Sprint(args...))
		default:
			t.t.Log(args...)
		}
	} else {
		// Add formatting directives for the callingPrefix and timeSuffix.
		format = "%v " + format + "%s"
		switch ltype {
		case errorLog:
			if t.expected(fmt.Sprintf(format, args...)) {
				t.t.Logf(format, args...)
			} else {
				t.t.Errorf(format, args...)
			}
		case fatalLog:
			panic(fmt.Sprintf(format, args...))
		default:
			t.t.Logf(format, args...)
		}
	}
}

// Update updates the testing.T that the testing logger logs to. Should be done
// before every test. It also initializes the tLogger if it has not already.
func (t *tLogger) Update(t *testing.T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.initialized {
		grpclog.SetLoggerV2(TLogger)
		t.initialized = true
	}
	t.t = t
	t.start = time.Now()
	t.errors = map[*regexp.Regexp]int{}
}

// ExpectError declares an error to be expected. For the next test, the first
// error log matching the expression (using FindString) will not cause the test
// to fail. "For the next test" includes all the time until the next call to
// Update(). Note that if an expected error is not encountered, this will cause
// the test to fail.
func (t *tLogger) ExpectError(expr string) {
	t.ExpectErrorN(expr, 1)
}

// ExpectErrorN declares an error to be expected n times.
func (t *tLogger) ExpectErrorN(expr string, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	re, err := regexp.Compile(expr)
	if err != nil {
		t.t.Error(err)
		return
	}
	t.errors[re] += n
}

// EndTest checks if expected errors were not encountered.
func (t *tLogger) EndTest(t *testing.T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for re, count := range t.errors {
		if count > 0 {
			t.Errorf("Expected error '%v' not encountered", re.String())
		}
	}
	t.errors = map[*regexp.Regexp]int{}
}

// expected determines if the error string is protected or not.
func (t *tLogger) expected(s string) bool {
	for re, count := range t.errors {
		if re.FindStringIndex(s) != nil {
			t.errors[re]--
			if count <= 1 {
				delete(t.errors, re)
			}
			return true
		}
	}
	return false
}

func (t *tLogger) Info(args ...any) {
	t.log(infoLog, 0, "", args...)
}

func (t *tLogger) Infoln(args ...any) {
	t.log(infoLog, 0, "", args...)
}

func (t *tLogger) Infof(format string, args ...any) {
	t.log(infoLog, 0, format, args...)
}

func (t *tLogger) InfoDepth(depth int, args ...any) {
	t.log(infoLog, depth, "", args...)
}

func (t *tLogger) Warning(args ...any) {
	t.log(warningLog, 0, "", args...)
}

func (t *tLogger) Warningln(args ...any) {
	t.log(warningLog, 0, "", args...)
}

func (t *tLogger) Warningf(format string, args ...any) {
	t.log(warningLog, 0, format, args...)
}

func (t *tLogger) WarningDepth(depth int, args ...any) {
	t.log(warningLog, depth, "", args...)
}

func (t *tLogger) Error(args ...any) {
	t.log(errorLog, 0, "", args...)
}

func (t *tLogger) Errorln(args ...any) {
	t.log(errorLog, 0, "", args...)
}

func (t *tLogger) Errorf(format string, args ...any) {
	t.log(errorLog, 0, format, args...)
}

func (t *tLogger) ErrorDepth(depth int, args ...any) {
	t.log(errorLog, depth, "", args...)
}

func (t *tLogger) Fatal(args ...any) {
	t.log(fatalLog, 0, "", args...)
}

func (t *tLogger) Fatalln(args ...any) {
	t.log(fatalLog, 0, "", args...)
}

func (t *tLogger) Fatalf(format string, args ...any) {
	t.log(fatalLog, 0, format, args...)
}

func (t *tLogger) FatalDepth(depth int, args ...any) {
	t.log(fatalLog, depth, "", args...)
}

func (t *tLogger) V(l int) bool {
	return l <= t.v
}
