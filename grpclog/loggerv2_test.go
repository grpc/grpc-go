/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpclog

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
)

func TestLoggerv2Severity(t *testing.T) {
	var buffers []*bytes.Buffer
	loggerv2 := newLoggerv2().(*loggerT)
	for i := 0; i < int(fatalLog); i++ {
		b := new(bytes.Buffer)
		buffers = append(buffers, b)
		loggerv2.m[i].SetOutput(b)
	}
	SetLoggerv2(loggerv2)

	Info(severityName[infoLog])
	Warning(severityName[warningLog])
	Error(severityName[errorLog])

	for i := 0; i < int(fatalLog); i++ {
		b := buffers[i]
		expected := regexp.MustCompile(fmt.Sprintf(`^%s: [0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} %s\n$`, severityName[i], severityName[i]))
		if m := expected.MatchReader(b); !m {
			t.Errorf("got: %v, want string in format of: %v\n", b.String(), severityName[i]+": 2016/10/05 17:09:26 "+severityName[i])
		}
	}
}
