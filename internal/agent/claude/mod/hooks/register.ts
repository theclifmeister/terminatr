// The terminatr mod (docs/SPEC.md §8.6, Mods). It reports the session's
// state to the server from its own events, in place of most command
// hooks: each transition, over the session's mod socket
// (TERMINATR_MOD_SOCKET, a Unix socket that speaks HTTP), and a beat
// every BEAT_MS with the state it holds; the server believes it while
// the beats come and falls back to its other sources when they stop.
// The turn lives in $.state, so a reload carries on (hooks/turn.ts).
//
// Over the same socket it takes the prompts the server queued for the
// session, one at a time and in order (hooks/deliver.ts): it long-polls
// GET /v1/prompts, acks the offer "taken", keeps its id in $.state, hands
// it to Claude ($.prompt.submit as the user's words, $.command.run for
// a slash command; Claude runs it once idle, leaving the prompt box
// alone), then acks "submitted", or "refused" so the server pastes it.
// An offer whose id is the one kept was handed on before a reload: it is
// only acked again. A loop that throws tells the server why (POST
// /v1/log) and starts again; one that stops going round is started again
// by the heartbeat; and a /clear, which may wipe $.state and starts no
// session.start, leaves the loop running (T82).
//
// It also follows the session's `tm watch --json` feed for the session's
// life and keeps the latest line in $.state. From it, unless [agent_mod] band
// is off (TERMINATR_BAND=off), it draws the band above the prompt and
// keeps a status entry under it: the thread's task, steps, current item,
// PR and what waits for the user; and it toasts when the PR's CI run
// finishes.
//
// After /clear and compaction it gives the thread or coordinator its
// context back (hooks/context.ts): fetched fresh from GET /v1/context
// when Claude reads the conversation's context blocks. The SessionStart
// command hook brings only a short line then, not a copy: its
// classic.SessionStart hook tells the server it ran (POST
// /v1/context/started) before the command hooks, which run beneath it.
// Another mod seated before it can skip both hooks; the command hooks
// then give the context themselves, and the server answers 204 here.
//
// In a thread session it gives the model the thread's tools,
// mcp__terminatr__report, __status, __steps and __done, served over the
// same socket (hooks/tools.ts): typed, checked by the server, run as the
// thread, and let through without a permission prompt. In a thread or a
// coordinator it also gives the model the tools enabled user mods offer
// the session (GET /v1/tools, mcp__terminatr__<mod>_<tool>), each call
// answered by the mod through the server.
//
// In a coordinator with open questions (tm ask) the status entry says so,
// "2 questions · /answer": the entry cannot be pressed, so /answer runs
// `tm ask open`, which queues the prompt that has the coordinator open
// them in its question dialog.
//
// It sends each AskUserQuestion menu to the server (`tm session ask`) and
// answers it with what `tm thread answer` gave, unless the user answers
// in the pane first.
//
// In a coordinator it also keeps the project's dashboard pane, /tm
// (hooks/pane.tsx), and registers /saveclear: `tm clear`, the save ask
// and then the clear that auto_clear does, which a /clear typed by hand
// skips.
//
// Its guard refuses tool calls that break the standing rules the server
// sends (hooks/guarded.ts, hooks/guard.ts).

import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { TerminatrTurn, TerminatrWatch } from '../types'
import { answers, question } from './ask'
import { drawBand } from './band'
import { withBlock } from './context'
import { errorText, handled, offerOf } from './deliver'
import type { Ack, Offer } from './deliver'
import { feed } from './feed'
import { guard } from './guarded'
import { registerPane, sawViewport } from './pane'
import { failed, POST_MS, postInit, refused, socketURL, timedOut, TOOL_MS, within } from './socket'
import { answer, body, MOD_TOOL, modTools, toolName, TOOLS } from './tools'
import type { ThreadTool } from './tools'
import { initialTurn, stateOf, step, waitKey } from './turn'
import { initialUsage, reportOf } from './usage'
import type { TurnUsageIn, UsageReport } from './usage'
import type { Seen } from './turn'
import { answeredText, bandShows, ciToast, clearedText, statusText } from './view'

const watch = atom({ plugin: 'terminatr', key: 'watch' } as const, null)
const band = atom({ plugin: 'terminatr', key: 'band' } as const, true)
const turn = atom({ plugin: 'terminatr', key: 'turn' } as const, initialTurn)
const usage = atom({ plugin: 'terminatr', key: 'usage' } as const, initialUsage)
const delivering = atom({ plugin: 'terminatr', key: 'delivering' } as const, '')
const deliverer = atom({ plugin: 'terminatr', key: 'deliverer' } as const, 0)

// CI_TOAST_MS is how long the CI toast stays: a result is worth more than
// the default 4 s, as the user is often reading elsewhere when it lands.
const CI_TOAST_MS = 15_000

// BEAT_MS is the heartbeat: the server's ModBeat (internal/agent).
const BEAT_MS = 10_000

// END_WAIT_MS bounds the wait for the exited report at session.end.
const END_WAIT_MS = 500

// POLL_S is how long the server holds a poll for a prompt; POLL_GAP_MS
// spaces polls that came back empty, RETRY_MS failed ones.
const POLL_S = 20
const POLL_GAP_MS = 250
const RETRY_MS = 5_000

// LOOP_STALL_MS is how long the prompt loop may go without a round, a
// prompt being handed on aside, before the heartbeat starts it again: a
// poll takes POLL_S at most.
const LOOP_STALL_MS = 60_000

// CONTEXT_WAIT_MS bounds the fetch of the role's context.
const CONTEXT_WAIT_MS = 2_000

// MARK_WAIT_MS bounds the SessionStart mark: past it the command hook
// gives the whole context, and the block is not added.
const MARK_WAIT_MS = 1_000

export const register: Register = on => {
  // The standing rules, checked on each tool call (hooks/guarded.ts),
  // whose hook also closes a call's permission prompt when it ends.
  guard(on)

  on('session.start', async ($, e, next) => {
    const started = await next(e)
    const bin = await $.env.get('TERMINATR_BIN')
    const id = await $.env.get('TERMINATR_SESSION')
    const isBand = (await $.env.get('TERMINATR_BAND')) !== 'off'
    await update($, band, () => isBand)
    await startReports($)
    const role = await $.env.get('TERMINATR_ROLE')
    if (socket && (role === 'thread' || role === 'coordinator')) await registerTools($, role)
    if (role === 'coordinator' && isBand) await $.command.register({ name: 'answer', description: "Open the coordinator's questions to you in a dialog" })
    if (role === 'coordinator') await $.command.register({ name: 'saveclear', description: 'Save what the coordinator learned, then clear its conversation (tm clear)' })
    if (bin && id) void follow($, bin, id, isBand)
    return started
  })

  // The role's context is our context block (below); after /clear and
  // compaction Claude is told to read the blocks again. The mark goes
  // first: the SessionStart command hook runs beneath, in next.
  on('classic.SessionStart', async ($, e, next) => {
    await markStart($)
    const r = await next(e)
    if (e.source === 'clear' || e.source === 'compact') $.ui.invalidate('prompt.context')
    return r
  })

  on('prompt.context', async ($, e, next) => {
    const r = await next(e)
    return { ...r, blocks: withBlock(r.blocks, (await fetchContext($)) ?? '') }
  })

  // The thread's tools: each call goes to the server, which answers what
  // the command printed or why it refused. A deny beneath (a rule of the
  // user's) stands; otherwise they run unasked, being the thread's own
  // reporting. They are listed up front, not behind ToolSearch.
  for (const t of TOOLS) {
    const tool = toolName(t)
    on('tool.call', { tool }, async ($, e) => callTool($, t, e, `; run tm ${t.name} in the shell`))
    on('tool.check', { tool }, async ($, e, next) => {
      const v = await next(e)
      return v.decision === 'deny' ? v : { decision: 'allow' }
    })
    on('tool.describe', { tool }, async ($, e, next) => ({ ...(await next(e)), isDeferred: false }))
  }

  // The user mods' tools (<mod>_<tool>): each call goes to the server,
  // which checks the input against the tool's schema and has the mod
  // answer. They run unasked, as the thread's own do: the user enabled
  // the mod and saw each tool's description when they did.
  on('tool.call', { tool: MOD_TOOL }, async ($, e, next) => {
    const t = offered.get(e.tool)
    if (!t) return next(e)
    return callTool($, t, e, '')
  })
  on('tool.check', { tool: MOD_TOOL }, async ($, e, next) => {
    const v = await next(e)
    return !offered.has(e.tool) || v.decision === 'deny' ? v : { decision: 'allow' }
  })

  // The menu is open while next(e) is pending; whichever answers first,
  // the user in the pane or tm, answers the call. Returning with next(e)
  // pending takes the menu down; ending the loop kills `tm session ask`,
  // which takes the question off the server.
  on('tool.call', { tool: 'AskUserQuestion' }, async ($, e, next) => {
    const bin = await $.env.get('TERMINATR_BIN')
    const id = await $.env.get('TERMINATR_SESSION')
    if (!bin || !id) return next(e)
    const menu = next(e)
    menu.catch(() => undefined)
    const ask = $.process.spawn({ argv: [bin, 'session', 'ask', id], input: question(e.tool, e.questions) })[Symbol.asyncIterator]()
    const fromTm = (async () => {
      let out = ''
      for (;;) {
        const r = await ask.next()
        if (r.done) return answers(out)
        if (r.value.stream === 'stdout') out += r.value.text
      }
    })().catch(() => null)
    const first = await Promise.race([menu.then(r => ({ r })), fromTm.then(a => ({ a }))])
    if ('r' in first) {
      void ask.return?.({ code: null, signal: null })
      return first.r
    }
    if (!first.a) return menu
    return { result: { questions: e.questions, answers: first.a } }
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    sawViewport(e.viewport)
    const w = await read($, watch)
    if (e.props.hasSurvey || !(await read($, band))) return next(e)
    if (!bandShows(w)) return next(e)
    return drawBand($.ui.resolve(e), e.props.bodyColumns, w)
  })

  // The status entry cannot be pressed, so a slash command opens them.
  on('command.run', { command: 'answer' }, async $ => {
    await answerQuestions($)
    return { text: 'Asked the coordinator to open your questions.' }
  })

  // /clear skips the save: /saveclear has tm take auto_clear's steps,
  // the save ask, then the clear once the coordinator is idle. It runs
  // tm itself rather than asking the model to: a clear can't be typed
  // from inside a turn, and the save ask then waits on nothing the
  // model might skip.
  on('command.run', { command: 'saveclear' }, async $ => ({ text: await saveClear($) }))

  on('turn.start', async ($, e, next) => {
    const r = await next(e)
    await saw($, { kind: 'turn.start' }, 'turn.start')
    return r
  })

  on('turn.complete', async ($, e, next) => {
    // A subagent's run is part of its spawner's turn.
    if (!e.agentId) await saw($, { kind: 'turn.complete', reason: e.reason }, 'turn.complete')
    void spent($, e.turnId, e.usage, !!e.agentId)
    return next(e)
  })

  // An AskUserQuestion menu is open for as long as its call runs.
  on('tool.call', { tool: 'AskUserQuestion' }, async ($, e, next) => {
    const key = waitKey(e.tool, e.agentId)
    await saw($, { kind: 'wait', key, reason: 'question' }, 'tool.call')
    try {
      return await next(e)
    } finally {
      await saw($, { kind: 'unwait', key }, 'tool.call')
    }
  })

  // A permission prompt opens unless a hook beneath decided; it closes
  // when the tool ends or is denied.
  on('classic.PermissionRequest', async ($, e, next) => {
    const r = await next(e)
    if (!r.decision) {
      const reason = e.tool_name === 'AskUserQuestion' ? 'question' : 'permission'
      await saw($, { kind: 'wait', key: waitKey(e.tool_name, e.agent_id), reason }, 'PermissionRequest')
    }
    return r
  })
  on('classic.PostToolUse', async ($, e, next) => {
    await saw($, { kind: 'unwait', key: waitKey(e.tool_name, e.agent_id) }, 'PostToolUse')
    return next(e)
  })
  on('classic.PostToolUseFailure', async ($, e, next) => {
    await saw($, { kind: 'unwait', key: waitKey(e.tool_name, e.agent_id) }, 'PostToolUseFailure')
    return next(e)
  })
  on('classic.PermissionDenied', async ($, e, next) => {
    await saw($, { kind: 'unwait', key: waitKey(e.tool_name, e.agent_id) }, 'PermissionDenied')
    return next(e)
  })

  // /compact runs no turn; ahead-of-time compaction doesn't hold the
  // session up.
  on('session.compact', async ($, e, next) => {
    if (e.trigger === 'precompute') return next(e)
    await saw($, { kind: 'compact', on: true }, 'session.compact')
    try {
      return await next(e)
    } finally {
      await saw($, { kind: 'compact', on: false }, 'session.compact')
    }
  })

  // The last report goes before the process does, or after END_WAIT_MS
  // at most.
  on('session.end', async ($, e, next) => {
    const { sent } = await saw($, e.reason === 'clear' ? { kind: 'clear' } : { kind: 'end' }, 'session.end')
    const cleared = e.reason === 'clear' ? clearedContext($) : Promise.resolve()
    await Promise.race([Promise.all([sent, cleared]), $.clock.sleep(END_WAIT_MS)])
    return next(e)
  })

  registerPane(on)
}

// answerQuestions has the coordinator open its questions (tm ask open):
// the server queues the prompt that does it, so it waits while a turn
// runs, and toasts what came of it.
async function answerQuestions($: EngineInterface) {
  const bin = await $.env.get('TERMINATR_BIN')
  if (!bin) return
  try {
    const r = await $.process.run([bin, 'ask', 'open'])
    $.ui.toast(answeredText(r.exitCode, r.stderr))
  } catch (err) {
    $.ui.toast(answeredText(null, String(err)))
  }
}

// saveClear has tm save and clear the coordinator (tm clear) and says
// what came of it. In this file: the engine follows $ into no import.
async function saveClear($: EngineInterface): Promise<string> {
  const bin = await $.env.get('TERMINATR_BIN')
  if (!bin) return clearedText(null, 'this session has no tm (TERMINATR_BIN)')
  try {
    const r = await $.process.run([bin, 'clear'])
    return clearedText(r.exitCode, r.stderr)
  } catch (err) {
    return clearedText(null, String(err))
  }
}

// callTool runs tool t's call e on the server (POST /v1/tools/<name>):
// what the command printed, or why it refused, is the tool's answer.
// Without the server, the deny says so, then hint.
async function callTool($: EngineInterface, t: ThreadTool, e: Record<string, unknown>, hint: string) {
  if (!socket) return { deny: `terminatr: this session has no mod socket${hint}` }
  try {
    const r = await within(
      $.http.fetch(`http://terminatr/v1/tools/${t.name}`, {
        method: 'POST', socketPath: socket, headers: { 'content-type': 'application/json' }, body: body(t, e),
      }),
      $.clock.sleep(TOOL_MS),
    )
    if (!r) return { deny: `terminatr: the server didn't answer in ${TOOL_MS / 1000} s${hint}` }
    return answer(r.status, r.text)
  } catch (err) {
    return { deny: `terminatr: the server didn't answer (${String(err)})${hint}` }
  }
}

// clearedContext tells the server the context is gone after /clear, so
// the panes show it at once; failures are logged and dropped.
async function clearedContext($: EngineInterface) {
  await post($, socket, '/v1/context/clear', 'context clear')
}

// spent sends what the turn cost, numbers only, a subagent's turns
// included: they are spend too (but not context: that is the session's). Failures are logged and dropped.
async function spent($: EngineInterface, turnId: string, u: TurnUsageIn | undefined, subagent: boolean) {
  if (!socket || !u) return
  let out: UsageReport | null = null
  try {
    const su = await $.session.usage()
    const ledger = su.cost?.usd
    await update($, usage, before => {
      const r = reportOf(turnId, u, ledger, before, su.context)
      out = r.report
      return r.now
    })
  } catch (err) {
    debug($, `terminatr: usage report: ${String(err)}`)
    return
  }
  if (!out) return
  // A subagent's context is its own, not the session's.
  if (subagent) (out as UsageReport).context = (out as UsageReport).context_window = 0
  await post($, socket, '/v1/usage', 'usage report', JSON.stringify(out))
}

// The channel: the socket, and one report in flight at a time, the
// latest waiting behind it (a report is the whole state, so the ones
// between can go).
let socket = ''
let sending: Promise<void> | null = null
let pending: string | null = null
let last = ''
let loops = 0

// The prompt loop: whether one runs, when it last went round, whether it
// is handing a prompt on (which may wait a whole turn), and whether the
// server said the session is gone.
let looping = false
let loopAt = 0
let handing = false
let loopGone = false

// startReports opens the channel, in a terminatr session with the mod's
// socket: it reports the state the turn is in (a reload carries on),
// then beats.
async function startReports($: EngineInterface) {
  socket = (await $.env.get('TERMINATR_MOD_SOCKET')) ?? ''
  if (!socket) return
  last = ''
  let now = initialTurn
  await update($, turn, t => (now = t.exited ? { ...initialTurn } : t))
  void report($, now, 'session.start')
  $.clock.every(BEAT_MS, () => {
    void read($, turn).then(t => report($, t, 'beat'))
    watchLoop($).catch(() => {})
  })
  loopGone = false
  // An unload ends the loop mid-call: nothing to report.
  keepDelivering($).catch(() => {})
}

// watchLoop starts the prompt loop again when it stopped going round
// while not handing a prompt on: nothing else would, and the beats would
// go on telling the server the mod takes its prompts.
async function watchLoop($: EngineInterface) {
  if (!looping || handing || loopGone) return
  const now = await $.clock.now()
  if (now - loopAt < LOOP_STALL_MS) return
  loopAt = now
  void modLog($, `the prompt loop made no round for ${Math.round(LOOP_STALL_MS / 1000)}s: started again`)
  keepDelivering($).catch(() => {})
}

// offered are the user mods' tools this session registered, by the name
// the model calls.
const offered = new Map<string, ThreadTool>()

// registerTools lists the session's tools for the model: a thread's own
// (one that fails leaves its shell command), then the user mods' tools
// the server offers the session (GET /v1/tools).
async function registerTools($: EngineInterface, role: string) {
  const reg = async (t: ThreadTool) => {
    try {
      await $.tool.register(t)
      return true
    } catch (err) {
      $.ui.log(`terminatr: tool ${t.name}: ${String(err)}`, { to: 'debug' })
      return false
    }
  }
  if (role === 'thread') for (const t of TOOLS) await reg(t)
  let listed: ThreadTool[] = []
  try {
    const r = await within($.http.fetch('http://terminatr/v1/tools', { method: 'GET', socketPath: socket }), $.clock.sleep(POST_MS))
    if (r) listed = modTools(r.status, r.text)
    else $.ui.log(timedOut("listing the mods' tools"), { to: 'debug' })
  } catch (err) {
    $.ui.log(`terminatr: listing the mods' tools: ${String(err)}`, { to: 'debug' })
  }
  offered.clear()
  for (const t of listed) if (await reg(t)) offered.set(toolName(t), t)
}

// keepDelivering runs the prompt loop until the session is gone or a
// newer loop took over (a reload's, or the heartbeat's), and runs it
// again when it throws, telling the server why. An unload ends it
// mid-call: nothing to report.
async function keepDelivering($: EngineInterface) {
  let me = 0
  try {
    me = (await $.clock.now()) * 1000 + (loops++ % 1000) + 1 // never 0, a wiped atom's
    looping = true
    loopAt = await $.clock.now()
    await update($, deliverer, () => me)
  } catch (err) {
    await modLog($, `the prompt loop didn't start: ${errorText(err)}`)
    return
  }
  for (;;) {
    try {
      await deliver($, me)
      return
    } catch (err) {
      await modLog($, `the prompt loop failed: ${errorText(err)}; starting it again`)
    }
    await $.clock.sleep(RETRY_MS)
  }
}

// mine says whether loop me still takes the prompts: another id in the
// atom is a newer loop's. A wiped atom (a /clear may start the session's
// state afresh) is claimed back.
async function mine($: EngineInterface, me: number): Promise<boolean> {
  const owner = await read($, deliverer)
  if (owner === me) return true
  if (owner) return false
  await update($, deliverer, v => v || me)
  return (await read($, deliverer)) === me
}

// deliver takes the server's prompts until the session is gone or a newer
// loop took over: a reload's loop, or one the heartbeat started; the old
// one stops at its next round.
async function deliver($: EngineInterface, me: number) {
  for (;;) {
    if (!(await mine($, me))) return
    loopAt = await $.clock.now()
    let r
    try {
      r = await $.http.fetch(`http://terminatr/v1/prompts?wait=${POLL_S}`, { socketPath: socket })
    } catch (err) {
      $.ui.log(`terminatr: prompts: ${String(err)}`, { to: 'debug' })
      await $.clock.sleep(RETRY_MS)
      continue
    }
    if (r.status === 410) {
      looping = false
      loopGone = true
      return
    }
    if (r.status === 204) {
      await $.clock.sleep(POLL_GAP_MS)
      continue
    }
    const offer = r.ok ? offerOf(r.text) : null
    if (!offer) {
      $.ui.log(`terminatr: prompts: ${r.status} ${r.text}`, { to: 'debug' })
      await $.clock.sleep(RETRY_MS)
      continue
    }
    if (!(await mine($, me))) return
    if (handled(offer, await read($, delivering))) {
      if (!(await ack($, offer.id, { result: 'submitted' }))) await $.clock.sleep(POLL_GAP_MS)
      continue
    }
    // Hand on only what the server let us take, and keep its id first,
    // so a reload from here on doesn't hand it on again.
    if (!(await ack($, offer.id, { result: 'taken' }))) {
      await $.clock.sleep(POLL_GAP_MS)
      continue
    }
    await update($, delivering, () => offer.id)
    handing = true
    const done = await handOn($, offer).finally(() => (handing = false))
    // Claude didn't take it: offered again, it is handed on again.
    if (done.result === 'refused') await update($, delivering, () => '')
    await ack($, offer.id, done)
  }
}

// handOn gives Claude the offer: queued, it runs once the session is
// idle; the prompt box keeps whatever is typed in it.
async function handOn($: EngineInterface, offer: Offer): Promise<Ack> {
  try {
    if (offer.kind === 'command') {
      await $.command.run({ command: offer.command, args: offer.args })
      return { result: 'submitted' }
    }
    const r = await $.prompt.submit({ text: offer.text, asUser: true })
    return r.drop === undefined ? { result: 'submitted' } : { result: 'refused', error: r.drop }
  } catch (err) {
    return { result: 'refused', error: errorText(err) }
  }
}

// ack tells the server what became of prompt id; true when it took it.
async function ack($: EngineInterface, id: string, a: Ack): Promise<boolean> {
  return post($, socket, `/v1/prompts/${encodeURIComponent(id)}/ack`, `prompt ${id} ${a.result}`, JSON.stringify(a))
}

// modLog puts a line in the server's log for this session (POST
// /v1/log); a failure goes to the debug log.
async function modLog($: EngineInterface, text: string) {
  $.ui.log(`terminatr: ${text}`, { to: 'debug' })
  await post($, socket, '/v1/log', 'log', JSON.stringify({ text }))
}

// markStart tells the server the mod's classic.SessionStart ran, so the
// command hook beneath says only where the context is; a failure is
// logged, and the command hook then gives the context itself.
async function markStart($: EngineInterface) {
  const sock = socket || ((await $.env.get('TERMINATR_MOD_SOCKET')) ?? '')
  if (!sock) return
  try {
    const r = await within($.http.fetch(socketURL('/v1/context/started'), postInit(sock)), $.clock.sleep(MARK_WAIT_MS))
    if (!r) debug($, timedOut('context mark'))
    else if (!r.ok) debug($, refused('context mark', r.status, r.text))
  } catch (err) {
    debug($, failed('context mark', err))
  }
}

// fetchContext is the role's context from the server: "" for a session
// outside a project, null when it can't be had.
async function fetchContext($: EngineInterface): Promise<string | null> {
  const sock = socket || ((await $.env.get('TERMINATR_MOD_SOCKET')) ?? '')
  if (!sock) return null
  try {
    const r = await Promise.race([
      $.http.fetch(socketURL('/v1/context'), { socketPath: sock }),
      $.clock.sleep(CONTEXT_WAIT_MS).then(() => null),
    ])
    if (!r) throw new Error('no answer')
    if (r.status === 204) return ''
    if (r.ok) return r.text
    $.ui.log(`terminatr: context: ${r.status} ${r.text}`, { to: 'debug' })
  } catch (err) {
    $.ui.log(`terminatr: context: ${String(err)}`, { to: 'debug' })
  }
  return null
}

// saw moves the turn on and reports where it got to, without waiting
// for the report: a hook never holds the session up on the server. It
// answers the report, for session.end.
async function saw($: EngineInterface, s: Seen, event: string): Promise<{ sent: Promise<void> }> {
  let now = initialTurn
  await update($, turn, t => (now = step(t, s)))
  return { sent: report($, now, event) }
}

// report sends the state t is in, unless it is the one sent last (a
// beat always goes), and resolves once it went.
function report($: EngineInterface, t: TerminatrTurn, event: string): Promise<void> {
  if (!socket) return Promise.resolve()
  const s = stateOf(t)
  const key = `${s.state}/${s.reason}`
  if (key === last && event !== 'beat') return sending ?? Promise.resolve()
  last = key
  pending = JSON.stringify({ ...s, event })
  if (!sending) {
    sending = (async () => {
      while (pending) {
        const body = pending
        pending = null
        await postState($, body)
      }
      sending = null
    })()
  }
  return sending
}

async function postState($: EngineInterface, body: string) {
  await post($, socket, '/v1/state', 'state report', body)
}

// post sends body, JSON, to path over sock (no body: an empty POST;
// hooks/socket.ts) and answers whether the server took it. A refusal or
// failure goes to the debug log as what and is dropped: a report never
// holds a hook up.
async function post($: EngineInterface, sock: string, path: string, what: string, body?: string): Promise<boolean> {
  if (!sock) return false
  try {
    const r = await within($.http.fetch(socketURL(path), postInit(sock, body)), $.clock.sleep(POST_MS))
    if (!r) {
      debug($, timedOut(what))
      return false
    }
    if (!r.ok) debug($, refused(what, r.status, r.text))
    return r.ok
  } catch (err) {
    debug($, failed(what, err))
    return false
  }
}

// debug puts text in the debug log, if there is one to put it in.
function debug($: EngineInterface, text: string) {
  try {
    $.ui.log(text, { to: 'debug' })
  } catch {
    // Nowhere to say it.
  }
}

// follow keeps the latest line of the feed, and the status entry and CI
// toast with it when the band is on. tm watch is started again when it
// ends before the session does (the server was busy or went away for a
// moment); the loop ends with the session (tm watch exits after the
// "exited" line) or when the module unloads.
async function follow($: EngineInterface, bin: string, id: string, isBand: boolean) {
  let last: TerminatrWatch | null = null
  for (;;) {
    let rest = ''
    try {
      for await (const { stream, text } of $.process.spawn({ argv: [bin, 'watch', '--session', id, '--json'] })) {
        if (stream !== 'stdout') continue
        const got = feed(rest, text)
        rest = got.rest
        if (got.watches.length === 0) continue
        for (const w of got.watches) {
          const toast = isBand ? ciToast(last, w) : undefined
          if (toast) $.ui.toast(toast, { timeoutMs: CI_TOAST_MS })
          last = w
        }
        const w = last
        await update($, watch, () => w)
        if (isBand) $.ui.status(statusText(w))
      }
    } catch (err) {
      $.ui.log(`terminatr: tm watch: ${String(err)}`, { to: 'debug' })
    }
    if (last?.session.state === 'exited') return
    try {
      await $.clock.sleep(RETRY_MS)
    } catch {
      return // the module unloaded (or there is no clock): stop quietly
    }
  }
}
