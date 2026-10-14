// The guard's hooks (hooks/guard.ts judges): every tool call is judged
// against the session's rules before it runs, in every permission mode,
// so a refusal comes ahead of any dialog. A refused call returns the
// rule's sentence as the tool's error, and the mod reports it to the
// server (POST /v1/denied), which journals it and tells the coordinator.
// A call no rule matches goes on to Claude's own checks. A PowerShell
// command the guard can't read is let through tool.call and asked in
// tool.check: a call Claude would allow goes to the dialog, or auto
// mode's classifier, with the guard's reason; one it would ask about
// or refuse keeps Claude's answer. An ask is not reported.
//
// The rules come from the server (GET /v1/rules), fetched at the first
// call after a load and again once they are a second old, so a change of
// the human's settings or of a guard mod reaches the session at once
// (the server answers from the rules it keeps while nothing they are
// made of changed: a fetch is cheap). Without the mod
// socket, or when the server can't say, the guard is off: the session
// keeps exactly the permission rules it has without the mod.

import type { EngineInterface, On } from 'claude-code'

import { judge, off, rulesOf } from './guard'
import type { Denial, Rules } from './guard'
import { debug, get, modSocket, post } from './socket'

// RULES_TTL_MS is how long fetched rules hold; FETCH_MS bounds a fetch.
const RULES_TTL_MS = 1_000
const FETCH_MS = 2_000

let cached: { at: number; rules: Promise<Rules> } | null = null

export function guard(on: On) {
  on('tool.call', async ($, e, next) => {
    const d = judge(await rulesNow($), e.tool, e as unknown as Record<string, unknown>)
    if (!d || d.ask) return next(e)
    void reportDenial($, e.tool, d)
    return { deny: d.message }
  }).catch(($, e, next) =>
    next.called ? next(e) : { deny: 'terminatr guard: the check of this call failed, so it was refused. Try again; if it keeps failing, say so.' },
  )

  // What $.tool.check asks gets the same answer, and a real call the
  // guard asks about its question here. A real call the guard refuses
  // never gets here: tool.call refused it first.
  on('tool.check', async ($, e, next) => {
    const input = e.input && typeof e.input === 'object' ? (e.input as Record<string, unknown>) : {}
    const d = judge(await rulesNow($), e.tool, input)
    if (!d) return next(e)
    if (!d.ask) return { decision: 'deny' as const, reason: d.message }
    const r = await next(e)
    return r.decision === 'allow' ? { decision: 'ask' as const, reason: d.message } : r
  }).catch(() => ({ decision: 'deny' as const }))
}

// rulesNow is the session's rules, fetched when there are none or they
// are older than RULES_TTL_MS. Rules it can't get are the guard off,
// never a refusal of every call.
async function rulesNow($: EngineInterface): Promise<Rules> {
  try {
    const socket = await modSocket($)
    if (!socket) return off
    const now = await $.clock.now()
    if (!cached || now - cached.at > RULES_TTL_MS) cached = { at: now, rules: fetchRules($, socket) }
    return await cached.rules
  } catch {
    cached = null
    return off
  }
}

async function fetchRules($: EngineInterface, socket: string): Promise<Rules> {
  const got = await get($, socket, '/v1/rules', FETCH_MS)
  if (got && got.ok) return rulesOf(got.text)
  // Asked again at the next call, not after the TTL.
  cached = null
  debug($, `terminatr: guard rules: ${got ? `${got.status} ${got.text}` : 'no answer'}`)
  return off
}

async function reportDenial($: EngineInterface, tool: string, d: Denial) {
  await post($, await modSocket($), '/v1/denied', 'guard report', JSON.stringify({ rule: d.rule, tool, summary: d.summary }))
}
