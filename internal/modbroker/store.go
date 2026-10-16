package modbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// MaxStore is the most a mod's store holds, as JSON.
const MaxStore = 1 << 20

// storeFile is a mod's store in its own folder.
const storeFile = "store.json"

type storeArgs struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func (b *Broker) readStore(id string) (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	data, err := os.ReadFile(filepath.Join(b.OwnDir(id), storeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, Failed("the store is broken: %v", err)
	}
	return m, nil
}

func (c *Call) storeKey() (storeArgs, error) {
	var a storeArgs
	if err := c.Decode(&a); err != nil {
		return a, err
	}
	return a, CheckKey(c.Method, a.Key)
}

// CheckKey refuses a store key that isn't 1 to 256 bytes; method is the
// call, for the message.
func CheckKey(method, key string) error {
	if key == "" || len(key) > 256 {
		return BadParams("%s: a key is 1 to 256 bytes", method)
	}
	return nil
}

// CheckStoreSize refuses a store whose JSON is over MaxStore.
func CheckStoreSize(store map[string]json.RawMessage) error {
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return checkStoreLen(len(data))
}

func checkStoreLen(n int) error {
	if n > MaxStore {
		return Denied("the store would be %d bytes, over its %d", n, MaxStore)
	}
	return nil
}

// store.get {key} → the value, or null.
func (b *Broker) storeGet(_ context.Context, c *Call) (any, error) {
	a, err := c.storeKey()
	if err != nil {
		return nil, err
	}
	b.store.Lock()
	defer b.store.Unlock()
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
// MaxStore.
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
	b.store.Lock()
	defer b.store.Unlock()
	m, err := b.readStore(id)
	if err != nil {
		return err
	}
	edit(m)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := checkStoreLen(len(data)); err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(b.OwnDir(id), storeFile), data, 0o600)
}
