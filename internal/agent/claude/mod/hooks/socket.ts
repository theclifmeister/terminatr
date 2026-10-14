// The mod socket (TERMINATR_MOD_SOCKET): a Unix socket that speaks HTTP
// to the server, the one way the hooks talk to it. modSocket reads where
// it is, post sends a report and logs what came of it, get asks with a
// deadline.

import type { EngineInterface } from 'claude-code'

type Answer = Awaited<ReturnType<EngineInterface['http']['fetch']>>

// modSocket is the session's mod socket, '' outside a terminatr session.
export async function modSocket($: EngineInterface): Promise<string> {
  return (await $.env.get('TERMINATR_MOD_SOCKET')) ?? ''
}

// post sends body, JSON, to path over socket (no body: an empty POST)
// and answers whether the server took it. A refusal or failure goes to
// the debug log as what and is dropped: a report never holds a hook up.
export async function post($: EngineInterface, socket: string, path: string, what: string, body?: string): Promise<boolean> {
  if (!socket) return false
  try {
    const r = await $.http.fetch(`http://terminatr${path}`, body === undefined
      ? { method: 'POST', socketPath: socket }
      : { method: 'POST', socketPath: socket, headers: { 'content-type': 'application/json' }, body })
    if (!r.ok) debug($, `terminatr: ${what}: ${r.status} ${r.text}`)
    return r.ok
  } catch (err) {
    debug($, `terminatr: ${what}: ${String(err)}`)
    return false
  }
}

// get asks path over socket and answers what the server said, null when
// it didn't within ms.
export function get($: EngineInterface, socket: string, path: string, ms: number): Promise<Answer | null> {
  return Promise.race([
    $.http.fetch(`http://terminatr${path}`, { socketPath: socket }),
    $.clock.sleep(ms).then(() => null),
  ])
}

// debug puts text in the debug log, if there is one to put it in.
export function debug($: EngineInterface, text: string) {
  try {
    $.ui.log(text, { to: 'debug' })
  } catch {
    // Nowhere to say it.
  }
}
