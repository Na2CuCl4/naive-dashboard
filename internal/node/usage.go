package node

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

const MaxUsageEvents = 1000

var journalName = regexp.MustCompile(`^[a-f0-9]{32}\.jsonl$`)

func readUsage(dir string, cursor map[string]int64) ([]Event, map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, errors.New("usage_unavailable")
	}
	names := make([]string, 0, len(entries))
	found := make(map[string]bool)
	for _, entry := range entries {
		if journalName.MatchString(entry.Name()) {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				return nil, nil, errors.New("invalid_usage_file")
			}
			names = append(names, entry.Name())
			found[entry.Name()] = true
		}
	}
	for name := range cursor {
		if !found[name] {
			return nil, nil, errors.New("usage_cursor_lost")
		}
	}
	sort.Strings(names)
	next := make(map[string]int64, len(names))
	for name, offset := range cursor {
		next[name] = offset
	}
	var events []Event
	for _, name := range names {
		offset := next[name]
		fd, err := syscall.Open(filepath.Join(dir, name), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, nil, errors.New("invalid_usage_file")
		}
		file := os.NewFile(uintptr(fd), name)
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			file.Close()
			return nil, nil, errors.New("invalid_usage_file")
		}
		if offset > info.Size() {
			file.Close()
			return nil, nil, errors.New("usage_cursor_lost")
		}
		if offset > 0 {
			var before [1]byte
			if _, err = file.ReadAt(before[:], offset-1); err != nil || before[0] != '\n' {
				file.Close()
				return nil, nil, errors.New("invalid_usage_cursor")
			}
		}
		if _, err = file.Seek(offset, io.SeekStart); err != nil {
			file.Close()
			return nil, nil, errors.New("invalid_usage_cursor")
		}
		reader := bufio.NewReaderSize(file, 4096)
		for len(events) < MaxUsageEvents {
			line, readErr := reader.ReadSlice('\n')
			if readErr == io.EOF {
				// Preserve a partial final line without advancing its cursor. A
				// crashed epoch stays readable while other epochs keep progressing.
				break
			}
			if readErr != nil {
				file.Close()
				return nil, nil, errors.New("invalid_usage_record")
			}
			var event Event
			if err = decodeStrict(line, &event); err != nil || event.Epoch != strings.TrimSuffix(name, ".jsonl") || event.SessionID == "" || len(event.SessionID) > 128 || !usernamePattern.MatchString(event.UserID) || event.Up < 0 || event.Down < 0 || event.At.IsZero() {
				file.Close()
				return nil, nil, errors.New("invalid_usage_record")
			}
			events = append(events, event)
			offset += int64(len(line))
		}
		file.Close()
		next[name] = offset
		if len(events) == MaxUsageEvents {
			break
		}
	}
	return events, next, nil
}
