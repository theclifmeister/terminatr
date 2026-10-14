// The reports the mod posts over its socket (hooks/socket.ts): every one
// goes the same way, and one the server refuses is logged and dropped.

import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'
import type { Engine } from 'claude-code/testing'

const SOCKET = '/run/s/s-7/mod.sock'

type Sent = { url: string; method?: string; socketPath?: string; headers?: unknown; body?: string }

// start runs session.start in a terminatr session whose server refuses
// every report.
async function start($: Engine, on: On) {
  const clock = mock.clock(on)
  mock.env(on, { TERMINATR_BIN: '/opt/tm', TERMINATR_SESSION: 's-7', TERMINATR_MOD_SOCKET: SOCKET })
  const sent: Sent[] = []
  const logs: unknown[] = []
  on('ui.log', async (_$, e) => {
    logs.push(e)
    return { value: undefined }
  })
  on('http.fetch', async (_$, e) => {
    const ok = (status: number, text = '') => ({ value: { status, ok: status < 300, headers: {}, text } })
    if (e.url.endsWith('/v1/rules') || e.url.includes('/v1/prompts')) return ok(204)
    sent.push({ url: e.url, method: e.init?.method, socketPath: e.init?.socketPath, headers: e.init?.headers, body: e.init?.body })
    return ok(500, 'down')
  })
  on('process.spawn', async function* () {
    return { value: { code: 0, signal: null } }
  })
  on('session.start', async (_$, e) => ({ cwd: e.cwd }))
  on('tool.call', { tool: 'Bash' }, async () => ({ result: { stdout: 'ran', stderr: '', interrupted: false } }))
  on('tool.check', async () => ({ decision: 'allow' as const }))
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  await clock.settle()
  return { clock, sent, logs }
}

test('a refused report is logged and dropped; each goes as JSON over the socket', async ($, on) => {
  const { clock, sent, logs } = await start($, on)
  const state = sent.find(s => s.url === 'http://terminatr/v1/state')
  expect(state?.method).toBe('POST')
  expect(state?.socketPath).toBe(SOCKET)
  expect(state?.headers).toEqual({ 'content-type': 'application/json' })
  expect(JSON.stringify(logs)).toContain('terminatr: state report: 500 down')

  // The hooks go on: a tool call runs.
  const ran = await $.tool.call({ tool: 'Bash', command: 'git status' })
  expect((ran as { result: { stdout: string } }).result.stdout).toBe('ran')
  await clock.settle()
})
