import type { TerminatrWatch } from '../types'

// ndjson takes what is left of the last piece and a new piece of a
// command's line-per-value JSON output: a piece may end inside a line or
// hold several. It answers the whole lines that parse and pass ok, in
// order, and the rest.
export function ndjson<T>(rest: string, text: string, ok: (v: unknown) => v is T): { values: T[]; rest: string } {
  const lines = (rest + text).split('\n')
  const tail = lines.pop() ?? ''
  const values: T[] = []
  for (const l of lines) {
    if (l.trim() === '') continue
    try {
      const v: unknown = JSON.parse(l)
      if (ok(v)) values.push(v)
    } catch {
      // not a line of the feed: skip it
    }
  }
  return { values, rest: tail }
}

// feed reads `tm watch --json` output (ndjson): the watches, and the rest.
export function feed(rest: string, text: string): { watches: TerminatrWatch[]; rest: string } {
  const got = ndjson(rest, text, isWatch)
  return { watches: got.values, rest: got.rest }
}

function isWatch(v: unknown): v is TerminatrWatch {
  const s = (v as { session?: unknown } | null)?.session
  return typeof s === 'object' && s !== null
}
