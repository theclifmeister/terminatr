package project

import (
	"regexp"
	"strings"
)

// The vault's items (docs/SPEC.md §5.1, Vault: write-back): what the
// human writes in Obsidian reaches the coordinator as an item, never as
// a change to the project's files. Its summary is tm's fixed words; the
// text stays where it is (Item.Path, Item.Hash) and tm inbox read shows
// it framed as data. It is not the user's word: any process of the
// user's, an agent too, can write the vault's folder.
const (
	// KindFromObsidian is a note, a line or a canvas card the human
	// tagged for tm, or their edit of a note only tm writes, which tm
	// saved before writing over it.
	KindFromObsidian = "from-obsidian"
	// KindVaultEdit is a change to PROJECT.md made outside tm: the
	// coordinator rereads it.
	KindVaultEdit = "vault-edit"
)

// The parts of a note a from-obsidian item is (Item.Part).
const (
	PartNote  = ""      // the whole note
	PartLine  = "line"  // a line tagged #for-tm
	PartCard  = "card"  // a canvas card tagged #for-tm: "card <id>"
	PartSaved = "saved" // an edit of a note tm writes, saved by tm
)

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// AddVaultItem writes a from-obsidian item about the text of hash at
// path, in the vault, which is part of it (PartNote and the others);
// summary makes its fixed words from the item's id.
func (p *Project) AddVaultItem(path, hash, part string, summary func(id string) string) (*Item, error) {
	it := &Item{Kind: KindFromObsidian, Subject: path, Path: path, Hash: hash, Part: part}
	if err := p.addItem(it, summary); err != nil {
		return nil, err
	}
	return it, nil
}

// Card is the canvas card id a from-obsidian item is about, "" when it
// is about no card.
func (it Item) Card() string {
	id, ok := strings.CutPrefix(it.Part, PartCard+" ")
	if !ok {
		return ""
	}
	return id
}
