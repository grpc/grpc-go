/*
 *
 * Copyright 2021 gRPC authors.
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

package googlecloud

import (
	"io"
	"os"
	"strings"
	"testing"
)

func setupManufacturerReader(testOS string, reader func() (io.Reader, error)) func() {
	tmpOS := runningOS
	tmpReader := manufacturerReader

	// Set test OS and reader function.
	runningOS = testOS
	manufacturerReader = reader
	return func() {
		runningOS = tmpOS
		manufacturerReader = tmpReader
	}
}

func setup(testOS string, testReader io.Reader) func() {
	reader := func() (io.Reader, error) {
		return testReader, nil
	}
	return setupManufacturerReader(testOS, reader)
}

func setupError(testOS string, err error) func() {
	reader := func() (io.Reader, error) {
		return nil, err
	}
	return setupManufacturerReader(testOS, reader)
}

func TestIsRunningOnGCE(t *testing.T) {
	for _, tc := range []struct {
		description string
		testOS      string
		testReader  io.Reader
		out         bool
	}{
		// Linux tests.
		{"linux: not a GCP platform", "linux", strings.NewReader("not GCP"), false},
		{"Linux: GCP platform (Google)", "linux", strings.NewReader("Google"), true},
		{"Linux: GCP platform (Google Compute Engine)", "linux", strings.NewReader("Google Compute Engine"), true},
		{"Linux: GCP platform (Google Compute Engine) with extra spaces", "linux", strings.NewReader("  Google Compute Engine        "), true},
		// Windows tests.
		{"windows: not a GCP platform", "windows", strings.NewReader("not GCP"), false},
		{"windows: GCP platform (Google)", "windows", strings.NewReader("Google"), true},
		{"windows: GCP platform (Google) with extra spaces", "windows", strings.NewReader("  Google     "), true},
	} {
		reverseFunc := setup(tc.testOS, tc.testReader)
		if got, want := isRunningOnGCE(), tc.out; got != want {
			t.Errorf("%v: isRunningOnGCE()=%v, want %v", tc.description, got, want)
		}
		reverseFunc()
	}
}

func TestIsRunningOnGCENoProductNameFile(t *testing.T) {
	reverseFunc := setupError("linux", os.ErrNotExist)
	if isRunningOnGCE() {
		t.Errorf("ErrNotExist: isRunningOnGCE()=true, want false")
	}
	reverseFunc()
}
