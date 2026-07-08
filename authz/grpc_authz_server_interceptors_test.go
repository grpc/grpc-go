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

package authz_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"google.golang.org/grpc/authz"
)

const defaultTestTimeout = 10 * time.Second

func createTmpPolicyFile(t *testing.T, dirSuffix string, policy []byte) string {
	t.Helper()

	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := os.MkdirTemp("", dirSuffix)
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Logf("Using tmpdir: %s", dir)
	// Write policy into file.
	filename := path.Join(dir, "policy.json")
	if err := os.WriteFile(filename, policy, os.ModePerm); err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", filename, err)
	}
	t.Logf("Wrote policy %s to file at %s", string(policy), filename)
	return filename
}

func (s) TestNewStatic(t *testing.T) {
	tests := map[string]struct {
		authzPolicy string
		wantErr     error
	}{
		"InvalidPolicyFailsToCreateInterceptor": {
			authzPolicy: `{}`,
			wantErr:     fmt.Errorf(`"name" is not present`),
		},
		"ValidPolicyCreatesInterceptor": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_all"
					}
				]
			}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := authz.NewStatic(test.authzPolicy); fmt.Sprint(err) != fmt.Sprint(test.wantErr) {
				t.Fatalf("NewStatic(%v) returned err: %v, want err: %v", test.authzPolicy, err, test.wantErr)
			}
		})
	}
}

func (s) TestNewFileWatcher(t *testing.T) {
	tests := map[string]struct {
		authzPolicy     string
		refreshDuration time.Duration
		wantErr         error
	}{
		"InvalidRefreshDurationFailsToCreateInterceptor": {
			refreshDuration: time.Duration(0),
			wantErr:         fmt.Errorf("authz: requires refresh interval(0s) greater than 0s"),
		},
		"InvalidPolicyFailsToCreateInterceptor": {
			authzPolicy:     `{}`,
			refreshDuration: time.Duration(1),
			wantErr:         fmt.Errorf(`"name" is not present`),
		},
		"ValidPolicyCreatesInterceptor": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_all"
					}
				]
			}`,
			refreshDuration: time.Duration(1),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			file := createTmpPolicyFile(t, name, []byte(test.authzPolicy))
			i, err := authz.NewFileWatcher(file, test.refreshDuration)
			if fmt.Sprint(err) != fmt.Sprint(test.wantErr) {
				t.Fatalf("NewFileWatcher(%v) returned err: %v, want err: %v", test.authzPolicy, err, test.wantErr)
			}
			if i != nil {
				i.Close()
			}
		})
	}
}

func (s) TestOnPolicyUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	updates := make(chan string, 1)
	onPolicyUpdate := func(s string) {
		updates <- s
	}

	content := `{"name": "foo1", "allow_rules":[{"name":"bar"}]}`
	file := createTmpPolicyFile(t, "onpolicyupdate", []byte(content))
	opts := authz.FileWatcherOptions{PolicyFile: file, RefreshDuration: 50 * time.Millisecond, OnPolicyUpdate: onPolicyUpdate}
	i, err := authz.NewFileWatcherWithOptions(opts)
	if err != nil {
		t.Fatalf("NewFileWatcherWithOptions(%v) returned err: %v", opts, err)
	}
	defer i.Close()

	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for policy update")
	case update := <-updates:
		if update != content {
			t.Fatalf("Unexpected contents on first load of policy file: got=%v, want=%v", update, content)
		}
	}

	// Tweak the file, expect an update.
	content = `{"name": "foo2", "allow_rules":[{"name":"bar"}]}`
	if err := os.WriteFile(file, []byte(content), os.ModePerm); err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", file, err)
	}

	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for policy update")
	case update := <-updates:
		if update != content {
			t.Fatalf("Unexpected contents after policy file changed: got=%v, want=%v", update, content)
		}
	}

	sCtx, sCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer sCancel()
	select {
	case <-updates:
		t.Fatal("OnPolicyUpdate callback invoked more times than expected")
	case <-sCtx.Done():
	}
}
