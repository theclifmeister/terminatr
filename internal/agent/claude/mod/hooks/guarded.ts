// The guard's hooks (hooks/guard.ts judges): every tool call is judged
// against the session's rules before it runs, in every permission mode,
// so a refusal comes ahead of any dialog. A refused call returns the
// rule's sentence as the tool's error, and the mod reports it to the
// server (POST /v1/denied), which journals it and tells the coordinator;
// the whole command line or file goes only to the guard's log, for the
// user to allow or keep refusing.
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
//
// A file tool's write is judged where it lands: the guard has no file
// system, so before judging one it asks the server (POST /v1/resolve)
// where each path it writes leads through links and junctions. A write
// the server can't place in time is refused, never judged as written.

import type { EngineInterface, On } from 'claude-code'

import { judge, off, rulesOf, writeTargets } from './guard'
import type { Denial, Rules } from './guard'
import { failed, FETCH_MS, postInit, refused, socketURL, timedOut, within } from './socket'

// RULES_TTL_MS is how long fetched rules hold; FETCH_MS bounds a fetch.
const RULES_TTL_MS = 1_000

// BACKOFF_MS is how long a fetch that got no answer is not tried again,
// and STALE_MS how long the last good rules stand in for rules the server
// can't give: a hung server costs one wait, not one per call, and a
// hiccup doesn't turn the guard off.
const BACKOFF_MS = 10_000
const STALE_MS = 60_000

// UNPLACED is what a write the server couldn't place is refused with.
const UNPLACED = 'terminatr guard: where this write lands could not be checked (the server did not answer), so it was refused. Try again; if it keeps failing, say so.'

let cached: { at: number; ttl: number; rules: Promise<Rules> } | null = null
let lastGood: { at: number; rules: Rules } | null = null

export function guard(on: On) {
  cached = null
  lastGood = null
  on('tool.call', async ($, e, next) => {
    const input = e as unknown as Record<string, unknown>
    const r = await placed($, await rulesNow($), e.tool, input)
    if (!r) return { deny: UNPLACED }
    const d = judge(r, e.tool, input)
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
    const r = await placed($, await rulesNow($), e.tool, input)
    if (!r) return { decision: 'deny' as const, reason: UNPLACED }
    const d = judge(r, e.tool, input)
    if (!d) return next(e)
    if (!d.ask) return { decision: 'deny' as const, reason: d.message }
    const c = await next(e)
    return c.decision === 'allow' ? { decision: 'ask' as const, reason: d.message } : c
  }).catch(() => ({ decision: 'deny' as const }))
}

// rulesNow is the session's rules, fetched when there are none or they
// are older than RULES_TTL_MS. Rules it can't get are the guard off,
// never a refusal of every call.
async function rulesNow($: EngineInterface): Promise<Rules> {
  try {
    const socket = (await $.env.get('TERMINATR_MOD_SOCKET')) ?? ''
    if (!socket) return off
    const now = await $.clock.now()
    if (!cached || now - cached.at > cached.ttl) cached = { at: now, ttl: RULES_TTL_MS, rules: fetchRules($, socket, now) }
    return await cached.rules
  } catch {
    cached = null
    return off
  }
}

async function fetchRules($: EngineInterface, socket: string, now: number): Promise<Rules> {
  const got = await Promise.race([
    $.http.fetch(socketURL('/v1/rules'), { socketPath: socket }),
    $.clock.sleep(FETCH_MS).then(() => null),
  ])
  if (got && got.ok) {
    const rules = rulesOf(got.text)
    lastGood = { at: now, rules }
    return rules
  }
  // No answer: not asked again for BACKOFF_MS. An answer that says no is
  // asked again at the next call, not after the TTL.
  const mine = cached
  if (got) cached = null
  else if (mine) mine.ttl = BACKOFF_MS
  log($, `terminatr: guard rules: ${got ? `${got.status} ${got.text}` : 'no answer'}`)
  return lastGood && now - lastGood.at < STALE_MS ? lastGood.rules : off
}

// placed is r with resolve answering where each file the call writes
// lands, as the server says (POST /v1/resolve); r itself when the call
// writes no file a rule judges. Null when the server can't say in time.
async function placed($: EngineInterface, r: Rules, tool: string, input: Record<string, unknown>): Promise<Rules | null> {
  const paths = writeTargets(r, tool, input)
  if (paths.length === 0) return r
  try {
    const socket = (await $.env.get('TERMINATR_MOD_SOCKET')) ?? ''
    const got = socket ? await within($.http.fetch(socketURL('/v1/resolve'), postInit(socket, JSON.stringify({ paths }))), $.clock.sleep(FETCH_MS)) : null
    if (!got) {
      log($, socket ? timedOut('guard resolve') : 'terminatr: guard resolve: no mod socket')
      return null
    }
    if (!got.ok) {
      log($, refused('guard resolve', got.status, got.text))
      return null
    }
    const lands = (JSON.parse(got.text) as { paths?: unknown }).paths
    if (!Array.isArray(lands) || lands.length !== paths.length || !lands.every(p => typeof p === 'string' && p !== '')) {
      log($, `terminatr: guard resolve: unexpected answer ${got.text}`)
      return null
    }
    const at = new Map(paths.map((p, i) => [p, lands[i] as string]))
    return { ...r, resolve: p => at.get(p) ?? p }
  } catch (err) {
    log($, failed('guard resolve', err))
    return null
  }
}

async function reportDenial($: EngineInterface, tool: string, d: Denial) {
  const socket = (await $.env.get('TERMINATR_MOD_SOCKET')) ?? ''
  if (!socket) return
  try {
    const r = await within($.http.fetch(socketURL('/v1/denied'), postInit(socket, JSON.stringify({ rule: d.rule, tool, summary: d.summary, subject: d.subject, floor: d.floor }))), $.clock.sleep(FETCH_MS))
    if (!r) log($, timedOut('guard report'))
    else if (!r.ok) log($, refused('guard report', r.status, r.text))
  } catch (err) {
    log($, failed('guard report', err))
  }
}

function log($: EngineInterface, text: string) {
  try {
    $.ui.log(text, { to: 'debug' })
  } catch {
    // Nowhere to say it.
  }
}
