package configloader

import (
	"crypto/sha256"
	"log"
	"os"
	"sync"
	"time"

	"ai-model-gateway/internal/core"
)

// Watcher polls a config file for changes and notifies subscribers.
// It uses SHA-256 content hashing to avoid spurious reloads.
type Watcher struct {
	path     string
	interval time.Duration

	mu       sync.RWMutex
	cfg      *core.Config
	lastHash [32]byte
	onChange []func(*core.Config)

	stopCh chan struct{}
}

// NewWatcher creates a config file watcher that checks for changes
// at the given polling interval.
func NewWatcher(path string, interval time.Duration) (*Watcher, error) {
	cfg, err := LoadFromFile(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		path:     path,
		interval: interval,
		cfg:      cfg,
		lastHash: sha256.Sum256(data),
		stopCh:   make(chan struct{}),
	}
	return w, nil
}

// Config returns the current config (thread-safe).
func (w *Watcher) Config() *core.Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg
}

// OnChange registers a callback that fires when the config is reloaded.
func (w *Watcher) OnChange(fn func(*core.Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onChange = append(w.onChange, fn)
}

// Start begins the polling loop in a background goroutine.
func (w *Watcher) Start() {
	go w.pollLoop()
}

// Stop terminates the polling loop.
func (w *Watcher) Stop() {
	select {
	case <-w.stopCh:
		return // already stopped
	default:
		close(w.stopCh)
	}
}

func (w *Watcher) pollLoop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *Watcher) check() {
	data, err := os.ReadFile(w.path)
	if err != nil {
		log.Printf("[config] watch read error: %v", err)
		return
	}

	hash := sha256.Sum256(data)
	w.mu.RLock()
	same := hash == w.lastHash
	w.mu.RUnlock()
	if same {
		return
	}

	cfg, err := LoadFromFile(w.path)
	if err != nil {
		log.Printf("[config] watch reload error (keeping old config): %v", err)
		return
	}

	w.mu.Lock()
	w.cfg = cfg
	w.lastHash = hash
	callbacks := make([]func(*core.Config), len(w.onChange))
	copy(callbacks, w.onChange)
	w.mu.Unlock()

	log.Printf("[config] config reloaded from %s", w.path)

	for _, fn := range callbacks {
		fn(cfg)
	}
}
