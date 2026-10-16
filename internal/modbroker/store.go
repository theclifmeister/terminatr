package modbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// maxStore is the most a mod's store holds, as JSON.
const maxStore = 1 << 20

// storeFile is a mod's store in its own folder.
const storeFile = "store.json"

type storeArgs struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// storeCache is a mod's store as last parsed, with the file's stamp:
// a read parses the file again only when it changed.
type storeCache struct {
	size int64
	mod  time.Time
	m    map[string]json.RawMessage
}

// storeLock is mod id's lock: one mod's store calls don't wait for another's.
func (b *Broker) storeLock(id string) *sync.Mutex {
	l, _ := b.stores.LoadOrStore(id, new(sync.Mutex))
	return l.(*sync.Mutex)
}

// readStore is mod id's store; the map is shared with the cache, so a
// caller that changes it must write it out (editStore) or drop it.
func (b *Broker) readStore(id string) (map[string]json.RawMessage, error) {
	path := filepath.Join(b.OwnDir(id), storeFile)
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		b.storeSeen.Delete(id)
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	if c, ok := b.storeSeen.Load(id); ok {
		c := c.(*storeCache)
		if c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
			return c.m, nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, Failed("the store is broken: %v", err)
	}
	b.storeSeen.Store(id, &storeCache{size: fi.Size(), mod: fi.ModTime(), m: m})
	return m, nil
}

func (c *Call) storeKey() (storeArgs, error) {
	var a storeArgs
	if err := c.Decode(&a); err != nil {
		return a, err
	}
	if a.Key == "" || len(a.Key) > 256 {
		return a, BadParams("%s: a key is 1 to 256 bytes", c.Method)
	}
	return a, nil
}

// store.get {key} → the value, or null.
func (b *Broker) storeGet(_ context.Context, c *Call) (any, error) {
	a, err := c.storeKey()
	if err != nil {
		return nil, err
	}
	l := b.storeLock(c.Mod.ID)
	l.Lock()
	defer l.Unlock()
	m, err := b.readStore(c.Mod.ID)
	if err != nil {
		return nil, err
	}
	if v, ok := m[a.Key]; ok {
		return v, nil
	}
	return nil, nil
}

// store.set {key, value}: value is any JSON; the store stays under
// maxStore.
func (b *Broker) storeSet(_ context.Context, c *Call) (any, error) {
	a, err := c.storeKey()
	if err != nil {
		return nil, err
	}
	if len(a.Value) == 0 {
		a.Value = json.RawMessage("null")
	}
	return nil, b.editStore(c.Mod.ID, func(m map[string]json.RawMessage) { m[a.Key] = a.Value })
}

// store.del {key}.
func (b *Broker) storeDel(_ context.Context, c *Call) (any, error) {
	a, err := c.storeKey()
	if err != nil {
		return nil, err
	}
	return nil, b.editStore(c.Mod.ID, func(m map[string]json.RawMessage) { delete(m, a.Key) })
}

func (b *Broker) editStore(id string, edit func(map[string]json.RawMessage)) error {
	l := b.storeLock(id)
	l.Lock()
	defer l.Unlock()
	cur, err := b.readStore(id)
	if err != nil {
		return err
	}
	// Edit a copy: a refused edit leaves the cached store as it was.
	m := maps.Clone(cur)
	edit(m)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(data) > maxStore {
		return Denied("the store would be %d bytes, over its %d", len(data), maxStore)
	}
	b.storeSeen.Delete(id)
	return fsx.WriteAtomic(filepath.Join(b.OwnDir(id), storeFile), data, 0o600)
}
