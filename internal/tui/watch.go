package tui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watcher signals when the configuration files change (or appear or disappear
// in the given directories). It groups bursts of events, like those an editor
// produces when saving.
type watcher struct {
	w      *fsnotify.Watcher
	Events chan struct{}
	done   chan struct{}
}

func newWatcher(files, dirs []string) (*watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{w: fw, Events: make(chan struct{}, 1), done: make(chan struct{})}
	// The directory of each file is watched, not the file: editors save with
	// "write temp and rename", which invalidates the watch on a specific file.
	seen := map[string]bool{}
	for _, f := range files {
		d := filepath.Dir(f)
		if !seen[d] {
			seen[d] = true
			_ = fw.Add(d)
		}
	}
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && !seen[d] {
			seen[d] = true
			_ = fw.Add(d)
		}
	}
	go w.loop()
	return w, nil
}

func (w *watcher) loop() {
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(150 * time.Millisecond)
			} else {
				timer.Reset(150 * time.Millisecond)
			}
			fire = timer.C
		case <-fire:
			fire = nil
			select {
			case w.Events <- struct{}{}:
			default:
			}
		case <-w.w.Errors:
		}
	}
}

func (w *watcher) Close() {
	close(w.done)
	w.w.Close()
}
