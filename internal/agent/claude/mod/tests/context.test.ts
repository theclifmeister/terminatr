import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

import { withBlock } from '../hooks/context'

const SOCKET = '/run/s/s-7/mod.sock'
const RULES = 'tm skill thread v0.9.0\n\nYou are one thread.\n'
const FRESH = RULES + '\nYou are thread t-1 of project demo.\nPR: #7 open, checks pass\n'

test('withBlock puts ours last, once', () => {
  const date = { name: 'currentDate', text: 'today' }
  expect(withBlock([date], 'x')).toEqual([date, { name: 'terminatr', text: 'x' }])
  expect(withBlock([{ name: 'terminatr', text: 'old' }, date], 'new')).toEqual([date, { name: 'terminatr', text: 'new' }])
  expect(withBlock([date], '')).toEqual([date])
})

// serve answers GET /v1/context with status and text, or fails, and
// takes POST /v1/context/started; calls lists the requests in order.
function serve(on: On, ctx: { status: number; text?: string } | 'down', calls: string[] = []) {
  mock.clock(on)
  mock.env(on, { TERMINATR_MOD_SOCKET: SOCKET })
  on('http.fetch', async (_$, e) => {
    expect(e.init?.socketPath).toBe(SOCKET)
    if (ctx === 'down') throw new Error('connection refused')
    const path = e.url.replace('http://terminatr', '')
    calls.push(`${e.init?.method ?? 'GET'} ${path}`)
    if (path === '/v1/context/started') return { value: { status: 204, ok: true, headers: {}, text: '' } }
    if (path !== '/v1/context') throw new Error('connection refused')
    return { value: { status: ctx.status, ok: ctx.status < 300, headers: {}, text: ctx.text ?? '' } }
  })
  on('prompt.context', async () => ({ blocks: [{ name: 'currentDate', text: 'today' }] }))
}

test('the context block is the server\'s, fresh', async ($, on) => {
  serve(on, { status: 200, text: FRESH })
  const r = await $.prompt.context({ blocks: [] })
  expect(r.blocks).toEqual([{ name: 'currentDate', text: 'today' }, { name: 'terminatr', text: FRESH }])
})

test('outside a project there is no block', async ($, on) => {
  serve(on, { status: 204 })
  const r = await $.prompt.context({ blocks: [] })
  expect(r.blocks).toEqual([{ name: 'currentDate', text: 'today' }])
})

test('a SessionStart brings no copy: the block is the server\'s alone', async ($, on) => {
  const calls: string[] = []
  serve(on, { status: 200, text: FRESH }, calls)
  // The bottom stands for the command hooks, which run beneath the mod:
  // the server has the mark by then.
  on('classic.SessionStart', async () => {
    expect(calls).toEqual(['POST /v1/context/started'])
    return { additionalContext: ['terminatr: see the context block'] }
  })
  const started = await $.classic.SessionStart({ source: 'clear' })
  expect(started.additionalContext).toEqual(['terminatr: see the context block'])
  const r = await $.prompt.context({ blocks: [] })
  expect(r.blocks).toEqual([{ name: 'currentDate', text: 'today' }, { name: 'terminatr', text: FRESH }])
})

test('a server that does not answer leaves no block', async ($, on) => {
  serve(on, 'down')
  const r = await $.prompt.context({ blocks: [] })
  expect(r.blocks).toEqual([{ name: 'currentDate', text: 'today' }])
})

test('a SessionStart whose mark gets no answer still starts', async ($, on) => {
  serve(on, 'down')
  on('classic.SessionStart', async () => ({ additionalContext: ['terminatr: the context'] }))
  const started = await $.classic.SessionStart({ source: 'startup' })
  expect(started.additionalContext).toEqual(['terminatr: the context'])
})

test('a block the server says a command hook gave is not added again', async ($, on) => {
  serve(on, { status: 204 })
  const r = await $.prompt.context({ blocks: [] })
  expect(r.blocks).toEqual([{ name: 'currentDate', text: 'today' }])
})
