package test

import (
	"fmt"
	"io/ioutil"
	"os"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/grpclog"
)

var testlogger *tlogger

func init() {
	testlogger = &tlogger{l: grpclog.NewLoggerV2(ioutil.Discard, os.Stderr, os.Stderr)}
	grpclog.SetLoggerV2(testlogger)
}

type tlogger struct {
	level int
	tSet  int32 // Accessed atomically
	t     *testing.T
	l     grpclog.LoggerV2
}

func (t *tlogger) setTestingT(tt *testing.T) {
	t.t = tt
	atomic.StoreInt32(&t.tSet, 1)
}

func (t *tlogger) isTestingTSet() bool {
	return atomic.LoadInt32(&t.tSet) == 1
}

func (t *tlogger) setLevel(l int) {
	t.level = l
}

func (t *tlogger) Info(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(args...)
		return
	}
	t.l.Info(args...)
}

func (t *tlogger) Infoln(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(fmt.Sprintln(args...))
		return
	}
	t.l.Infoln(args...)
}

func (t *tlogger) Infof(format string, args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Logf(format, args...)
		return
	}
	t.l.Infof(format, args...)
}

func (t *tlogger) Warning(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(args...)
		return
	}
	t.l.Warning(args...)
}

func (t *tlogger) Warningln(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(fmt.Sprintln(args...))
		return
	}
	t.l.Warningln(args...)
}

func (t *tlogger) Warningf(format string, args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Logf(format, args...)
		return
	}
	t.l.Warningf(format, args...)
}

func (t *tlogger) Error(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(args...)
		return
	}
	t.l.Error(args...)
}

func (t *tlogger) Errorln(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Log(fmt.Sprintln(args...))
		return
	}
	t.l.Errorln(args...)
}

func (t *tlogger) Errorf(format string, args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Logf(format, args...)
		return
	}
	t.l.Errorf(format, args...)
}

func (t *tlogger) Fatal(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Fatal(args...)
		return
	}
	t.l.Fatal(args...)
}

func (t *tlogger) Fatalln(args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Fatal(fmt.Sprintln(args...))
		return
	}
	t.l.Fatalln(args...)
}

func (t *tlogger) Fatalf(format string, args ...interface{}) {
	if t.isTestingTSet() {
		t.t.Fatalf(format, args...)
		return
	}
	t.l.Fatalf(format, args...)
}

func (t *tlogger) V(l int) bool {
	return l <= t.level
}
