// Package mdfile reads and writes markdown files with front matter, using
// a lock file and atomic rename for every write (docs/SPEC.md §5.1).
//
// Front matter is TOML between +++ lines or YAML between --- lines; both
// read, for good (old archives and hand-made files keep reading). Join
// writes YAML (yamlwrite.go), and ToYAML turns a file's TOML front matter
// into it (the one-time migration). YAML is read strictly (yaml.go).
//
// Locks are advisory flock(2) locks on a hidden sibling ".<file>.lock",
// so every writer of a file must go through Lock, Update, Write or Append.
// Readers don't lock: a rename is atomic, so a reader sees the old or the
// new file.
package mdfile
