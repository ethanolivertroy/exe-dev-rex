package rexapp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

const sample = `{"formatVersion":1,"records":[{"icon":"server.rack","id":"AAA","lastHostID":"h1","lastInstanceID":"i1","locators":["box.tail1.ts.net"],"name":"Box","preferredURL":"http://box.tail1.ts.net:7297"}]}`

func TestParseAddRemoveRoundTrip(t *testing.T) {
	l, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if string(l.Encode()) != sample {
		t.Fatalf("round trip changed the JSON:\n%s", l.Encode())
	}
	if err := l.Add("BBB", "vm - exe.dev", "vm.tail1.ts.net", "server.rack", "h2", "i2"); err != nil {
		t.Fatal(err)
	}
	if err := l.Add("CCC", "VM - EXE.DEV", "other", "x", "", ""); err == nil {
		t.Fatal("duplicate label accepted")
	}
	if !l.Has("vm.tail1.ts.net") || len(l.Hosts()) != 2 {
		t.Fatalf("add failed: %+v", l.Hosts())
	}
	if gone := l.Remove("vm.tail1.ts.net"); len(gone) != 1 || gone[0] != "BBB" {
		t.Fatalf("remove: %v", gone)
	}
	if string(l.Encode()) != sample {
		t.Fatalf("add+remove didn't restore the list:\n%s", l.Encode())
	}
}

func TestParseRefusesUnknownFormats(t *testing.T) {
	for _, bad := range []string{
		`{"formatVersion":2,"records":[]}`,
		`{"formatVersion":1}`,
		`{"formatVersion":1,"records":[{"id":"A","name":"x","locators":[],"newField":1}]}`,
		`{"formatVersion":1,"records":[{"id":"A","name":"x"}]}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// TestLiveListRoundTrips checks this Mac's real list survives decode/encode
// unchanged (read-only). Key order may differ; the app parses it as JSON.
func TestLiveListRoundTrips(t *testing.T) {
	raw, err := ReadRaw(context.Background())
	if err != nil {
		t.Skip("no Rex prefs here:", err)
	}
	l, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var was, now any
	if json.Unmarshal(raw, &was) != nil || json.Unmarshal(l.Encode(), &now) != nil || !reflect.DeepEqual(was, now) {
		t.Fatalf("live list doesn't round-trip:\nwas %s\nnow %s", raw, l.Encode())
	}
}
