package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func eventLine(epoch, session string, up int64, final bool) []byte {
	data, _ := json.Marshal(Event{Epoch: epoch, SessionID: session, UserID: "u_demo", Up: up, Down: up * 2, Final: final, At: time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)})
	return append(data, '\n')
}

func TestUsageEmptyDirectoryPreservesExplicitCursor(t *testing.T) {
	s, _ := fakeService(t)
	response := s.Handle(Request{Action: "usage"})
	if !response.OK {
		t.Fatalf("empty usage directory: %+v", response)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Response
	if err = json.Unmarshal(data, &roundTrip); err != nil || roundTrip.Cursor == nil || len(roundTrip.Cursor) != 0 {
		t.Fatalf("empty cursor omitted by JSON transport: %s (%v)", data, err)
	}
}

func TestUsageReplayPartialTailAndRestart(t *testing.T) {
	dir := t.TempDir()
	epoch := strings.Repeat("a", 32)
	nextEpoch := strings.Repeat("b", 32)
	first := eventLine(epoch, "1", 17, false)
	second := eventLine(epoch, "1", 42, true)
	writeTest(t, filepath.Join(dir, epoch+".jsonl"), append(append([]byte{}, first...), second[:30]...))
	events, cursor, err := readUsage(dir, nil)
	if err != nil || len(events) != 1 || cursor[epoch+".jsonl"] != int64(len(first)) {
		t.Fatalf("partial tail: %v %v %v", events, cursor, err)
	}
	replayed, repeatedCursor, err := readUsage(dir, nil)
	if err != nil || !reflect.DeepEqual(events, replayed) || !reflect.DeepEqual(cursor, repeatedCursor) {
		t.Fatal("same acknowledged cursor did not replay deterministically")
	}
	// Simulate a hard process stop with a torn final line. The restarted epoch
	// must remain readable without acknowledging the torn record.
	writeTest(t, filepath.Join(dir, nextEpoch+".jsonl"), eventLine(nextEpoch, "1", 9, true))
	events, updated, err := readUsage(dir, cursor)
	if err != nil || len(events) != 1 || events[0].Epoch != nextEpoch || updated[epoch+".jsonl"] != cursor[epoch+".jsonl"] {
		t.Fatalf("restart epoch behind torn record: %+v %v %v", events, updated, err)
	}
	// An ordinary append completes the active writer's partial line exactly once.
	file, err := os.OpenFile(filepath.Join(dir, epoch+".jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(second[30:])
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	events, finalCursor, err := readUsage(dir, updated)
	if err != nil || len(events) != 1 || !events[0].Final || events[0].Up != 42 {
		t.Fatalf("completed final record: %+v %v", events, err)
	}
	events, _, err = readUsage(dir, finalCursor)
	if err != nil || len(events) != 0 {
		t.Fatalf("acknowledged records reappeared: %+v %v", events, err)
	}
}

func TestUsageBoundedBatchAndLostCursor(t *testing.T) {
	dir := t.TempDir()
	epoch := strings.Repeat("c", 32)
	name := epoch + ".jsonl"
	line := eventLine(epoch, "same", 5, true)
	writeTest(t, filepath.Join(dir, name), []byte(strings.Repeat(string(line), MaxUsageEvents+2)))
	events, cursor, err := readUsage(dir, nil)
	if err != nil || len(events) != MaxUsageEvents {
		t.Fatalf("bounded batch: %d %v", len(events), err)
	}
	events, cursor, err = readUsage(dir, cursor)
	if err != nil || len(events) != 2 {
		t.Fatalf("batch continuation: %d %v", len(events), err)
	}
	writeTest(t, filepath.Join(dir, name), line)
	if _, _, err = readUsage(dir, cursor); err == nil || err.Error() != "usage_cursor_lost" {
		t.Fatalf("truncated journal: %v", err)
	}
	if err = os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = readUsage(dir, cursor); err == nil || err.Error() != "usage_cursor_lost" {
		t.Fatalf("removed journal: %v", err)
	}
}

func TestUsageRejectsBadRecordsAndPaths(t *testing.T) {
	epoch := strings.Repeat("d", 32)
	for _, test := range []struct{ name, body string }{
		{"invalid-json", "{bad}\n"},
		{"wrong-epoch", string(eventLine(strings.Repeat("e", 32), "1", 1, true))},
		{"negative-bytes", string(eventLine(epoch, "1", -1, true))},
		{"oversize-line", strings.Repeat(" ", 5000) + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTest(t, filepath.Join(dir, epoch+".jsonl"), []byte(test.body))
			if _, _, err := readUsage(dir, nil); err == nil || err.Error() != "invalid_usage_record" {
				t.Fatalf("invalid record accepted: %v", err)
			}
		})
	}
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "target"), eventLine(epoch, "1", 1, true))
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, epoch+".jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readUsage(dir, nil); err == nil || err.Error() != "invalid_usage_file" {
		t.Fatalf("symlink accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, epoch+".jsonl")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, epoch+".jsonl"), eventLine(epoch, "1", 1, true))
	if _, _, err := readUsage(dir, map[string]int64{epoch + ".jsonl": 1}); err == nil || err.Error() != "invalid_usage_cursor" {
		t.Fatalf("non-line cursor accepted: %v", err)
	}
}
