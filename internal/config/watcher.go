package config

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	Debounce time.Duration
}

func (w Watcher) WatchFile(ctx context.Context, path string, onChange func(Config)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		return err
	}

	if w.Debounce == 0 {
		w.Debounce = 200 * time.Millisecond
	}

	var timer *time.Timer

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt := <-watcher.Events:
			if filepath.Clean(evt.Name) != filepath.Clean(path) {
				continue
			}
			if evt.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.Debounce, func() {
				if cfg, err := LoadFromFile(path); err == nil {
					onChange(cfg)
				}
			})
		case <-watcher.Errors:
		}
	}
}
