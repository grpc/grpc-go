/*
 *
 * Copyright 2017 gRPC authors.
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

package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// logFuncStr is a string used via testCheckLogContainsFuncStr to test the
// logger output.
const logFuncStr = "called-func"

func makeSprintfErr(t *testing.T) func(format string, a ...any) string {
	return func(string, ...any) string {
		t.Errorf("got: sprintf called on io.Discard logger, want: expensive sprintf to not be called for io.Discard")
		return ""
	}
}

func makeSprintErr(t *testing.T) func(a ...any) string {
	return func(...any) string {
		t.Errorf("got: sprint called on io.Discard logger, want: expensive sprint to not be called for io.Discard")
		return ""
	}
}

// checkLogContainsFuncStr checks that the logger buffer logBuf contains
// logFuncStr.
func checkLogContainsFuncStr(t *testing.T, logBuf []byte) {
	if !bytes.Contains(logBuf, []byte(logFuncStr)) {
		t.Errorf("got '%v', want logger func to be called and print '%v'", string(logBuf), logFuncStr)
	}
}

// checkBufferWasWrittenAsExpected checks that the log buffer buf was written as expected,
// per the discard, logTYpe, msg, and isJSON arguments.
func checkBufferWasWrittenAsExpected(t *testing.T, buf *bytes.Buffer, discard bool, logType string, msg string, isJSON bool) {
	bts, err := buf.ReadBytes('\n')
	if discard {
		if err == nil {
			t.Fatalf("got '%v', want discard %v to not write", string(bts), logType)
		} else if err != io.EOF {
			t.Fatalf("got '%v', want discard %v buffer to be EOF", err, logType)
		}
	} else {
		if err != nil {
			t.Fatalf("got '%v', want non-discard %v to not error", err, logType)
		} else if !bytes.Contains(bts, []byte(msg)) {
			t.Fatalf("got '%v', want non-discard %v buffer contain message '%v'", string(bts), logType, msg)
		}
		if isJSON {
			obj := map[string]string{}
			if err := json.Unmarshal(bts, &obj); err != nil {
				t.Fatalf("got '%v', want non-discard json %v to unmarshal", err, logType)
			} else if _, ok := obj["severity"]; !ok {
				t.Fatalf("got '%v', want non-discard json %v to have severity field", "missing severity", logType)

			} else if jsonMsg, ok := obj["message"]; !ok {
				t.Fatalf("got '%v', want non-discard json %v to have message field", "missing message", logType)

			} else if !strings.Contains(jsonMsg, msg) {
				t.Fatalf("got '%v', want non-discard json %v buffer contain message '%v'", string(bts), logType, msg)
			}
		}
	}
}

// check if b is in the format of:
//
//	2017/04/07 14:55:42 WARNING: WARNING
func checkLogForSeverity(s int, b []byte) error {
	expected := regexp.MustCompile(fmt.Sprintf(`^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} %s: %s\n$`, severityName[s], severityName[s]))
	if m := expected.Match(b); !m {
		return fmt.Errorf("got: %v, want string in format of: %v", string(b), severityName[s]+": 2016/10/05 17:09:26 "+severityName[s])
	}
	return nil
}

func TestLoggerV2Severity(t *testing.T) {
	buffers := []*bytes.Buffer{new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)}
	l := NewLoggerV2(buffers[infoLog], buffers[warningLog], buffers[errorLog], LoggerV2Config{})

	l.Info(severityName[infoLog])
	l.Warning(severityName[warningLog])
	l.Error(severityName[errorLog])

	for i := 0; i < fatalLog; i++ {
		buf := buffers[i]
		// The content of info buffer should be something like:
		//  INFO: 2017/04/07 14:55:42 INFO
		//  WARNING: 2017/04/07 14:55:42 WARNING
		//  ERROR: 2017/04/07 14:55:42 ERROR
		for j := i; j < fatalLog; j++ {
			b, err := buf.ReadBytes('\n')
			if err != nil {
				t.Fatalf("level %d: %v", j, err)
			}
			if err := checkLogForSeverity(j, b); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestLoggerV2PrintFuncDiscardOnlyInfo ensures that logs at the INFO level are
// discarded when set to io.Discard, while logs at other levels (WARN, ERROR)
// are still printed. It does this by using a custom error function that raises
// an error if the logger attempts to print at the INFO level, ensuring early
// return when io.Discard is used.
func TestLoggerV2PrintFuncDiscardOnlyInfo(t *testing.T) {
	buffers := []*bytes.Buffer{nil, new(bytes.Buffer), new(bytes.Buffer)}
	logger := NewLoggerV2(io.Discard, buffers[warningLog], buffers[errorLog], LoggerV2Config{})
	loggerTp := logger.(*loggerT)

	// test that output doesn't call expensive printf funcs on an io.Discard logger
	sprintf = makeSprintfErr(t)
	sprint = makeSprintErr(t)
	sprintln = makeSprintErr(t)

	loggerTp.output(infoLog, "something")

	sprintf = fmt.Sprintf
	sprint = fmt.Sprint
	sprintln = fmt.Sprintln

	loggerTp.output(errorLog, logFuncStr)
	warnB, err := buffers[warningLog].ReadBytes('\n')
	if err != nil {
		t.Fatalf("level %v: %v", warningLog, err)
	}
	checkLogContainsFuncStr(t, warnB)

	errB, err := buffers[errorLog].ReadBytes('\n')
	if err != nil {
		t.Fatalf("level %v: %v", errorLog, err)
	}
	checkLogContainsFuncStr(t, errB)
}

func TestLoggerV2PrintFuncNoDiscard(t *testing.T) {
	buffers := []*bytes.Buffer{new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)}
	logger := NewLoggerV2(buffers[infoLog], buffers[warningLog], buffers[errorLog], LoggerV2Config{})
	loggerTp := logger.(*loggerT)

	loggerTp.output(errorLog, logFuncStr)

	infoB, err := buffers[infoLog].ReadBytes('\n')
	if err != nil {
		t.Fatalf("level %v: %v", infoLog, err)
	}
	checkLogContainsFuncStr(t, infoB)

	warnB, err := buffers[warningLog].ReadBytes('\n')
	if err != nil {
		t.Fatalf("level %v: %v", warningLog, err)
	}
	checkLogContainsFuncStr(t, warnB)

	errB, err := buffers[errorLog].ReadBytes('\n')
	if err != nil {
		t.Fatalf("level %v: %v", errorLog, err)
	}
	checkLogContainsFuncStr(t, errB)
}

// TestLoggerV2PrintFuncAllDiscard tests that discard loggers don't log.
func TestLoggerV2PrintFuncAllDiscard(t *testing.T) {
	logger := NewLoggerV2(io.Discard, io.Discard, io.Discard, LoggerV2Config{})
	loggerTp := logger.(*loggerT)

	sprintf = makeSprintfErr(t)
	sprint = makeSprintErr(t)
	sprintln = makeSprintErr(t)

	// test that printFunc doesn't call the log func on discard loggers
	// makeLogFuncErr will fail the test if it's called
	loggerTp.output(infoLog, logFuncStr)
	loggerTp.output(warningLog, logFuncStr)
	loggerTp.output(errorLog, logFuncStr)

	sprintf = fmt.Sprintf
	sprint = fmt.Sprint
	sprintln = fmt.Sprintln
}

func TestLoggerV2PrintFuncAllCombinations(t *testing.T) {
	const (
		print int = iota
		printf
		println
	)

	type testDiscard struct {
		discardInf  bool
		discardWarn bool
		discardErr  bool

		printType  int
		formatJSON bool
	}

	discardName := func(td testDiscard) string {
		strs := []string{}
		if td.discardInf {
			strs = append(strs, "discardInfo")
		}
		if td.discardWarn {
			strs = append(strs, "discardWarn")
		}
		if td.discardErr {
			strs = append(strs, "discardErr")
		}
		if len(strs) == 0 {
			strs = append(strs, "noDiscard")
		}
		return strings.Join(strs, " ")
	}
	var printName = []string{
		print:   "print",
		printf:  "printf",
		println: "println",
	}
	var jsonName = map[bool]string{
		true:  "json",
		false: "noJson",
	}

	discardTests := []testDiscard{}
	for _, di := range []bool{true, false} {
		for _, dw := range []bool{true, false} {
			for _, de := range []bool{true, false} {
				for _, pt := range []int{print, printf, println} {
					for _, fj := range []bool{true, false} {
						discardTests = append(discardTests, testDiscard{discardInf: di, discardWarn: dw, discardErr: de, printType: pt, formatJSON: fj})
					}
				}
			}
		}
	}

	for _, discardTest := range discardTests {
		testName := fmt.Sprintf("%v %v %v", jsonName[discardTest.formatJSON], printName[discardTest.printType], discardName(discardTest))
		t.Run(testName, func(t *testing.T) {
			cfg := LoggerV2Config{FormatJSON: discardTest.formatJSON}

			buffers := []*bytes.Buffer{new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)}
			writers := []io.Writer{buffers[infoLog], buffers[warningLog], buffers[errorLog]}
			if discardTest.discardInf {
				writers[infoLog] = io.Discard
			}
			if discardTest.discardWarn {
				writers[warningLog] = io.Discard
			}
			if discardTest.discardErr {
				writers[errorLog] = io.Discard
			}
			logger := NewLoggerV2(writers[infoLog], writers[warningLog], writers[errorLog], cfg)

			msgInf := "someinfo"
			msgWarn := "somewarn"
			msgErr := "someerr"
			if discardTest.printType == print {
				logger.Info(msgInf)
				logger.Warning(msgWarn)
				logger.Error(msgErr)
			} else if discardTest.printType == printf {
				logger.Infof("%v", msgInf)
				logger.Warningf("%v", msgWarn)
				logger.Errorf("%v", msgErr)
			} else if discardTest.printType == println {
				logger.Infoln(msgInf)
				logger.Warningln(msgWarn)
				logger.Errorln(msgErr)
			}

			// verify the test Discard, log type, message, and json arguments were logged as-expected

			checkBufferWasWrittenAsExpected(t, buffers[infoLog], discardTest.discardInf, "info", msgInf, cfg.FormatJSON)
			checkBufferWasWrittenAsExpected(t, buffers[warningLog], discardTest.discardWarn, "warning", msgWarn, cfg.FormatJSON)
			checkBufferWasWrittenAsExpected(t, buffers[errorLog], discardTest.discardErr, "error", msgErr, cfg.FormatJSON)
		})
	}
}

func TestLoggerV2Fatal(t *testing.T) {
	const (
		print   = "print"
		println = "println"
		printf  = "printf"
	)
	printFuncs := []string{print, println, printf}

	exitCalls := []int{}

	if reflect.ValueOf(exit).Pointer() != reflect.ValueOf(os.Exit).Pointer() {
		t.Error("got: exit isn't os.Exit, want exit var to be os.Exit")
	}

	exit = func(code int) {
		exitCalls = append(exitCalls, code)
	}
	defer func() {
		exit = os.Exit
	}()

	for _, printFunc := range printFuncs {
		buffers := []*bytes.Buffer{new(bytes.Buffer), new(bytes.Buffer), new(bytes.Buffer)}
		logger := NewLoggerV2(buffers[infoLog], buffers[warningLog], buffers[errorLog], LoggerV2Config{})
		switch printFunc {
		case print:
			logger.Fatal("fatal")
		case println:
			logger.Fatalln("fatalln")
		case printf:
			logger.Fatalf("fatalf %d", 42)
		default:
			t.Errorf("unknown print type '%v'", printFunc)
		}

		checkBufferWasWrittenAsExpected(t, buffers[errorLog], false, "fatal", "fatal", false)
		if len(exitCalls) == 0 {
			t.Error("got: no exit call, want fatal log to call exit")
		}
		exitCalls = []int{}
	}
}
