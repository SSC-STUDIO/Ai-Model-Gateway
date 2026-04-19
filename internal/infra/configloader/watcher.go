package configloader

import (
	"crypto/sha256"
	"log"
	"os"
	"sync"
	"time"

	"ai-model-gateway/internal/core"
)

var (
	watcherSettleInterval = 25 * time.Millisecond
	watcherSettleChecks   = 3
)

// Watcher polls a config file for changes and notifies subscribers.
// It uses SHA-256 content hashing and briefly re-reads changed files so
// transient in-place rewrites do not spuriously trigger reloads.
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg, err := loadFromBytes(path, data)
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
	lastHash := w.currentHash()

	data, hash, err := readHashedFile(w.path)
	if err != nil {
		log.Printf("[config] watch read error: %v", err)
		return
	}
	if hash == lastHash {
		return
	}

	for i := 0; i < watcherSettleChecks; i++ {
		time.Sleep(watcherSettleInterval)

		nextData, nextHash, err := readHashedFile(w.path)
		if err != nil {
			log.Printf("[config] watch read error: %v", err)
			return
		}
		if nextHash == lastHash {
			return
		}
		if nextHash == hash {
			data = nextData
			hash = nextHash
			break
		}

		data = nextData
		hash = nextHash

		if i == watcherSettleChecks-1 {
			return
		}
	}

	cfg, err := loadFromBytes(w.path, data)
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

func (w *Watcher) currentHash() [32]byte {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastHash
}

func readHashedFile(path string) ([]byte, [32]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return data, sha256.Sum256(data), nil
}
