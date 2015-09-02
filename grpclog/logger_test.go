/*
 *
 * Copyright 2015, Google Inc.
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
	"strings"
	"testing"
)

func testLogger(t testing.TB, testCase func(l Logger), wantContains ...string) {
	buf := new(bytes.Buffer)
	testCase(&logfmt{out: buf})
	got := buf.String()
	for _, want := range wantContains {
		if !strings.Contains(got, "time") {
			t.Fatalf("no time set in %q", got)
		}
		if !strings.Contains(got, want) {
			t.Errorf("want=%q", want)
			t.Errorf("got =%q", got)
			t.Fatal("want string to contains")
		}
	}
}

func TestPlainLog(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world")
	}, `"msg"="hello world"`+"\n")
}

func TestSuperfluousNewline(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world\n")
	}, `"msg"="hello world"`+"\n")
}

func TestNewLinesLog(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world")
		l.Print("bonjour le monde")
	},
		`"msg"="hello world"`+"\n",
		`"msg"="bonjour le monde"`+"\n",
	)
}

func TestManyLinesLog(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world")
		l.Print("bonjour le monde")
	},
		`"lvl"="info" "msg"="hello world"`+"\n",
		`"lvl"="info" "msg"="bonjour le monde"`+"\n",
	)
}

func TestLinesLogWithFields(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world")
		annotated := l.With("herp", "derp")
		annotated.Print("bonjour le monde")
		annotated.Print("foobar")
		l.Print("foobaz")
	},
		`"lvl"="info" "msg"="hello world"`+"\n",
		`"lvl"="info" "msg"="bonjour le monde" "herp"="derp"`+"\n",
		`"lvl"="info" "msg"="foobar" "herp"="derp"`+"\n",
		`"lvl"="info" "msg"="foobaz"`+"\n",
	)
}

func TestLinesLogWithNestedFields(t *testing.T) {
	testLogger(t, func(l Logger) {
		l.Print("hello world")
		annotated := l.With("herp", "derp")
		annotated.Print("bonjour le monde")
		superannotated := annotated.With("derp?", "herp!")
		annotated.Print("foobar")
		superannotated.Print("ho lala!")
		l.Print("foobaz")
	},
		`"lvl"="info" "msg"="hello world"`+"\n",
		`"lvl"="info" "msg"="bonjour le monde" "herp"="derp"`+"\n",
		`"lvl"="info" "msg"="foobar" "herp"="derp"`+"\n",
		`"lvl"="info" "msg"="ho lala!" "herp"="derp" "derp?"="herp!"`+"\n",
		`"lvl"="info" "msg"="foobaz"`+"\n",
	)
}
