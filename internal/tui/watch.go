package tui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watcher avisa cuando cambian los archivos de la configuración (o aparecen
// o desaparecen en los directorios indicados). Agrupa ráfagas de eventos, como
// los que produce un editor al guardar.
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
	// Se vigila el directorio de cada archivo y no el archivo: los editores
	// guardan con «escribir temporal y renombrar», que invalida la vigilancia
	// de un archivo concreto.
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
