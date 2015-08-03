package etcdnaming

import (
	"testing"
)

func TestEtcdParsing(t *testing.T) {
	for _, test := range []struct {
		// input
		body string
		// expected
		expected map[string]string
	}{
		{"", nil},
		{`{"action":"set","node":{"key":"/foo","value":"bar5","modifiedIndex":30,"createdIndex":30}}`, map[string]string{"/foo": "bar5"}},
		{`{"action":"set","node":{"key":"/foo_dir/foo2","value":"bar2","modifiedIndex":59,"createdIndex":59},"prevNode":{"key":"/foo_dir/foo2","value":"bar","modifiedIndex":58,"createdIndex":58}}`, map[string]string{"/foo_dir/foo2": "bar2"}},
		{`{"action":"get","node":{"key":"/foo_dir","dir":true,"nodes":[{"key":"/foo_dir/foo2","value":"bar2","modifiedIndex":33,"createdIndex":33},{"key":"/foo_dir/bar_dir","dir":true,"nodes":[{"key":"/foo_dir/bar_dir/bar1","value":"a","modifiedIndex":35,"createdIndex":35},{"key":"/foo_dir/bar_dir/bar2","value":"b","modifiedIndex":36,"createdIndex":36}],"modifiedIndex":34,"createdIndex":34},{"key":"/foo_dir/bar_dir2","dir":true,"nodes":[{"key":"/foo_dir/bar_dir2/bar3","value":"c","modifiedIndex":39,"createdIndex":39}],"modifiedIndex":38,"createdIndex":38},{"key":"/foo_dir/foo6","value":"newbar2","modifiedIndex":48,"createdIndex":48},{"key":"/foo_dir/foo3","value":"newbar2","modifiedIndex":50,"createdIndex":50},{"key":"/foo_dir/foo","value":"newbar","modifiedIndex":42,"createdIndex":42}],"modifiedIndex":32,"createdIndex":32}}`, map[string]string{"/foo_dir/foo2": "bar2", "/foo_dir/bar_dir/bar1": "a", "/foo_dir/bar_dir/bar2": "b", "/foo_dir/bar_dir2/bar3": "c", "/foo_dir/foo6": "newbar2", "/foo_dir/foo3": "newbar2", "/foo_dir/foo": "newbar"}},
	} {
		out := parser(test.body)
		if len(test.expected) != len(out) {
			t.Fatalf("want result length %v, get %v", len(test.expected), len(out))
		}
		for key, _ := range out {
			if test.expected[key] != out[key] {
				t.Fatalf("expected result error!, want %v get %v", test.expected[key], out[key])
			}
		}
	}
}
