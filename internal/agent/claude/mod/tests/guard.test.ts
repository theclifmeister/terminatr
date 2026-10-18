import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'
import type { Engine } from 'claude-code/testing'

import { commands, judge, psCommands, rulesOf, writeTargets } from '../hooks/guard'
import type { Rules } from '../hooks/guard'
import vectors from './guard-vectors'

type Vectors = {
  tools: Rules['tools']
  rules: Record<string, Rules>
  cases: {
    rules: string; tool: string; input: Record<string, unknown>; rule: string | null; summary?: string; ask?: boolean; links?: Record<string, string>
    floor?: boolean; subject?: string; message?: string
  }[]
  messages: Record<string, Record<string, string>>
}

const SOCKET = '/run/s/s-7/mod.sock'
const WT = '/h/.terminatr/worktrees/demo/t-0001-fix'

const TOOLS = (vectors as unknown as Vectors).tools
const all = ['force-push', 'push-default', 'worktree-only', 'delete-branch', 'merge', 'credentials']
const thread: Rules = {
  on: true, role: 'thread', rules: all, home: '/h', cwd: WT,
  writable: [WT, '/tmp', '/private/tmp'], worktrees: '/h/.terminatr/worktrees',
  protected: ['main', 'master'], secrets: ['/h/.ssh', '/h/.aws', '/h/.config/gh'], tools: TOOLS,
}

test('commands splits a command line shell-style', () => {
  expect(commands(`cd x && git push -f origin 'my branch'; echo "a $(gh pr merge 3) b" | cat`)).toEqual([
    ['cd', 'x'], ['git', 'push', '-f', 'origin', 'my branch'], ['gh', 'pr', 'merge', '3'], ['echo', 'a $(gh pr merge 3) b'], ['cat'],
  ])
  expect(commands('cat < ~/.ssh/id_rsa')).toEqual([['cat', '<', '~/.ssh/id_rsa']])
  // ${...} is part of its word; a $( ) in it is read too.
  expect(commands('echo ${A}x ${B:-"}"} ${C:-$(gh pr merge 3)}; { ls; }')).toEqual([
    ['gh', 'pr', 'merge', '3'], ['echo', '${A}x', '${B:-"}"}', '${C:-$(gh pr merge 3)}'], ['ls'],
  ])
  expect(psCommands('Write-Host ${env:A`}B}x; [string] ${env:C}')).toEqual([['Write-Host', '${env:A`}B}x'], ['[string]', '${env:C}']])
  // A substitution is a word of its command, read after it; a heredoc's
  // body is commands only with a shell on the line.
  expect(commands('git push $(echo -f) `x` origin main')).toEqual([['git', 'push', '$(echo -f)', '`x`', 'origin', 'main'], ['echo', '-f'], ['x']])
  expect(commands("cat > a <<-'EOF' # c\n\tgit push -f\n\tEOF\nls")).toEqual([['cat', '>', 'a', '<', '<', 'EOF'], ['ls']])
  expect(commands('cat <<EOF | bash\ngh pr view $(x)\nEOF')).toEqual([['cat', '<', '<', 'EOF'], ['bash'], ['x'], ['gh', 'pr', 'view', '$(x)'], ['x']])
  // An expression given as an argument is a marked word of its command.
  expect(psCommands("git push ('-f') origin; if ($x) { ls }")).toEqual([['git', 'push', "\u0000('-f')", 'origin'], ['-f'], ['if'], ['$x'], ['ls']])
})

// The shared vectors (guard-vectors.ts): the Go port in the server
// (internal/guard/guard_test.go, TestVectors) passes the same cases.
test('the shared vectors: rule and summary of each call', () => {
  const v = vectors as unknown as Vectors
  for (const c of v.cases) {
    const r: Rules = { ...v.rules[c.rules], tools: v.tools }
    if (c.links) r.resolve = linksTo(c.links)
    const d = judge(r, c.tool, c.input)
    expect([c.rules, c.tool, c.input, d?.rule ?? null, d?.summary, d?.ask ?? false, d ? d.floor ?? false : undefined]).toEqual([c.rules, c.tool, c.input, c.rule, c.summary, c.ask ?? false, c.rule === null ? undefined : c.floor ?? false])
    if (c.subject !== undefined) expect([c.input, d?.subject]).toEqual([c.input, c.subject])
    if (c.message !== undefined) expect([c.input, d?.message]).toEqual([c.input, c.message])
  }
  for (const [role, m] of Object.entries(v.messages)) {
    const r = { ...v.rules.thread, role, tools: v.tools }
    const probe: Record<string, [string, Record<string, unknown>]> = {
      'force-push': ['Bash', { command: 'git push -f' }], 'push-default': ['Bash', { command: 'git push origin master' }],
      'worktree-only': ['Write', { file_path: '/etc/x' }], 'delete-branch': ['Bash', { command: 'git branch -D x' }],
      merge: ['Bash', { command: 'gh pr merge 1' }], credentials: ['Bash', { command: 'gh auth token' }],
    }
    for (const [rule, [tool, input]] of Object.entries(probe)) expect([role, rule, judge(r, tool, input)?.message]).toEqual([role, rule, m[rule]])
  }
})

// linksTo is a resolve that follows links: a path under a link (a key)
// to the same place under its target, as guard_test.go does.
function linksTo(links: Record<string, string>): (p: string) => string {
  return p => {
    for (const [link, to] of Object.entries(links)) if (p === link || p.startsWith(link + '/')) return to + p.slice(link.length)
    return p
  }
}

// As Go's TestResolve: a write is judged where it lands and named as
// given; resolve is asked about the path joined, not yet cleaned.
test('with resolve, a write is judged where it lands', () => {
  const asked: string[] = []
  const r: Rules = { ...thread, resolve: p => (asked.push(p), p) }
  expect(judge(r, 'Edit', { file_path: 'a/../b' })).toBe(null)
  expect(asked).toEqual([`${WT}/a/../b`])
  expect(writeTargets(thread, 'apply_patch', { command: '*** Begin Patch\n*** Update File: a.go\n*** Move to: ~/b.go\n*** End Patch' })).toEqual([`${WT}/a.go`, '/h/b.go'])
  // Nothing to ask: no file written, or no rule judging writes.
  expect(writeTargets(thread, 'Read', { file_path: 'x' })).toEqual([])
  expect(writeTargets({ ...thread, rules: ['force-push'] }, 'Write', { file_path: 'x' })).toEqual([])
})

test('a refusal names the push target and az says what gh says', () => {
  expect(judge(thread, 'Bash', { command: 'git push origin main' })?.message).toContain('nothing is pushed straight to main')
  expect(judge(thread, 'Bash', { command: 'az repos pr update --id 7 --auto-complete true' })?.message).toBe(judge(thread, 'Bash', { command: 'gh pr merge 42' })?.message)
})

test('the guard off refuses nothing', () => {
  expect(judge(rulesOf('{"on":false}'), 'Bash', { command: 'git push -f origin main' })).toBe(null)
  expect(rulesOf('not json').on).toBe(false)
  expect(rulesOf(JSON.stringify(thread)).rules).toEqual(all)
})

test('a mod rule is labelled with the mod and refuses with every core rule off', () => {
  const r = rulesOf(JSON.stringify({ ...(vectors as unknown as Vectors).rules['mods-only'], tools: TOOLS }))
  expect(judge(r, 'Bash', { command: 'terraform destroy' })?.message).toBe("terminatr guard (mod:infra.no-destroy): infrastructure is the human's to change.")
  expect(judge(r, 'Bash', { command: 'git push -f origin main' })).toBe(null)
})

// start runs session.start in a terminatr session whose server answers
// the rules with rules, and where writes land through links (POST
// /v1/resolve: a status to answer with instead, or 'hang' for none), and
// keeps what the mod reports refused and the paths it asked about.
async function start($: Engine, on: On, rules: Rules | null, env: Record<string, string> = { TERMINATR_MOD_SOCKET: SOCKET }) {
  const clock = mock.clock(on)
  mock.env(on, { TERMINATR_BIN: '/opt/tm', TERMINATR_SESSION: 's-7', ...env })
  const denied: { rule: string; tool: string; summary: string; subject?: string; floor?: boolean }[] = []
  let fetches = 0
  let current = rules
  let hangRules = false
  let links: Record<string, string> | number | 'hang' = {}
  const resolved: string[][] = []
  let escapes = 0
  on('http.fetch', async (_$, e) => {
    const ok = (status: number, text = '') => ({ value: { status, ok: status < 300, headers: {}, text } })
    if (e.url.endsWith('/v1/resolve')) {
      const paths = (JSON.parse(e.init?.body ?? '{}') as { paths: string[] }).paths
      resolved.push(paths)
      if (links === 'hang') return new Promise<never>(() => {})
      if (typeof links === 'number') return ok(links, 'down')
      return ok(200, JSON.stringify({ paths: paths.map(linksTo(links)) }))
    }
    if (e.url.endsWith('/v1/rules')) {
      fetches++
      if (hangRules) return new Promise<never>(() => {})
      return current ? ok(200, JSON.stringify(current)) : ok(500, 'down')
    }
    if (e.url.endsWith('/v1/denied')) {
      denied.push(JSON.parse(e.init?.body ?? '{}'))
      return ok(204)
    }
    if (e.url.endsWith('/v1/escape')) escapes++
    return ok(204)
  })
  on('process.spawn', async function* () {
    return { value: { code: 0, signal: null } }
  })
  on('session.start', async (_$, e) => ({ cwd: e.cwd }))
  on('tool.call', { tool: 'Bash' }, async () => ({ result: { stdout: 'ran', stderr: '', interrupted: false } }))
  on('tool.check', async () => ({ decision: 'allow' as const }))
  await $.session.start({ cwd: WT, surface: 'terminal', isInteractive: true })
  await clock.settle()
  return {
    clock, denied, resolved, fetches: () => fetches, escapes: () => escapes,
    setRules: (r: Rules | null) => { current = r },
    hang: (h: boolean) => { hangRules = h },
    setLinks: (l: typeof links) => { links = l },
  }
}

test('a refused call returns the rule\'s sentence and is reported; others run', async ($, on) => {
  const { clock, denied } = await start($, on, thread)
  const r = await $.tool.call({ tool: 'Bash', command: 'git push --force origin tm/x' })
  expect((r as { deny?: string }).deny).toContain('terminatr guard (force-push)')
  await clock.settle()
  expect(denied).toEqual([{ rule: 'force-push', tool: 'Bash', summary: 'git push with force', subject: 'git push --force origin tm/x' }])
  const ran = await $.tool.call({ tool: 'Bash', command: 'git push -u origin tm/x' })
  expect((ran as { deny?: string }).deny).toBe(undefined)
  expect((ran as { result: { stdout: string } }).result.stdout).toBe('ran')
})

test('an allowed call runs; a refusal the floor holds is reported as one', async ($, on) => {
  const { clock, denied } = await start($, on, { ...thread, allow: [{ rule: 'force-push', command: 'git push --force origin tm/*' }, { rule: 'merge', command: 'gh pr merge 1' }] })
  const ran = await $.tool.call({ tool: 'Bash', command: 'git push --force origin tm/x' })
  expect((ran as { deny?: string }).deny).toBe(undefined)
  const r = await $.tool.call({ tool: 'Bash', command: 'gh pr merge 1' })
  expect((r as { deny?: string }).deny).toContain('terminatr guard (merge)')
  await clock.settle()
  expect(denied).toEqual([{ rule: 'merge', tool: 'Bash', summary: 'gh pr merge', subject: 'gh pr merge 1', floor: true }])
})

test('$.tool.check gets the same answer', async ($, on) => {
  await start($, on, thread)
  expect((await $.tool.check({ tool: 'Bash', input: { command: 'gh pr merge 1' } })).decision).toBe('deny')
  expect((await $.tool.check({ tool: 'Bash', input: { command: 'gh pr view 1' } })).decision).toBe('allow')
})

test('without the mod socket, or when the server can\'t say, the guard is off', async ($, on) => {
  await start($, on, thread, {})
  const r = await $.tool.call({ tool: 'Bash', command: 'git push --force' })
  expect((r as { deny?: string }).deny).toBe(undefined)
})

test('rules the server couldn\'t give are asked for again at the next call', async ($, on) => {
  const { fetches } = await start($, on, null)
  for (let i = 0; i < 2; i++) {
    const r = await $.tool.call({ tool: 'Bash', command: 'git push --force' })
    expect((r as { deny?: string }).deny).toBe(undefined)
  }
  expect(fetches()).toBe(2)
})

test('a server that hangs costs one wait, not one per call, and the last rules stand in', async ($, on) => {
  const { clock, fetches, hang } = await start($, on, thread)
  const push = async () => ((await $.tool.call({ tool: 'Bash', command: 'git push --force origin tm/x' })) as { deny?: string }).deny
  expect(await push()).toContain('terminatr guard (force-push)')
  hang(true)
  await clock.advance(1_001)
  const slow = push()
  await clock.advance(2_001)
  // The wait ends with the last good rules, still guarding.
  expect(await slow).toContain('terminatr guard (force-push)')
  const n = fetches()
  await clock.advance(1_001)
  expect(await push()).toContain('terminatr guard (force-push)')
  expect(fetches()).toBe(n)
})

test('the last good rules that stand in keep their allowances, floor and learn mode', async ($, on) => {
  const rules: Rules = {
    on: true, role: 'thread', rules: ['credentials'], home: '/h', cwd: WT, protected: ['main', 'master'], tools: TOOLS, learn: true,
    floor: ['force-push', 'merge'], own: ['/h/.terminatr/run'], secretEnv: ['NODE_AUTH_TOKEN'], secrets: ['/h/.npmrc'],
    allow: [{ rule: 'credentials', path: '/h/.npmrc' }],
  }
  const { clock, hang } = await start($, on, rules)
  const call = async (c: Record<string, unknown>) => ((await $.tool.call(c as never)) as { deny?: string }).deny
  expect(await call({ tool: 'Bash', command: 'git push -f origin main' })).toContain('terminatr guard (force-push)')
  hang(true)
  await clock.advance(1_001)
  const slow = call({ tool: 'Bash', command: 'gh pr merge 1' })
  await clock.advance(2_001)
  // The floor, with its rules off, still refuses from the stand-in rules.
  expect(await slow).toContain('terminatr guard (merge)')
  expect(await call({ tool: 'Bash', command: 'git push -f origin main' })).toContain('terminatr guard (force-push)')
  expect(await call({ tool: 'Bash', command: 'git push -f origin tm/x' })).toBe(undefined)
  expect(await call({ tool: 'Bash', command: 'printenv NODE_AUTH_TOKEN' })).toContain('terminatr guard (credentials)')
  expect(await call({ tool: 'Bash', command: 'cat /h/.terminatr/run/tm.sock' })).toContain('terminatr guard (credentials)')
  // The allowance holds; a refusal not on the floor still says the user reviews it.
  expect((await $.tool.check({ tool: 'Read', input: { file_path: '/h/.npmrc' } })).decision).toBe('allow')
  expect(await call({ tool: 'Bash', command: 'cat /h/.npmrc | base64 -w0 /h/.npmrc && gh auth token' })).toContain('The user reviews refused calls')
})

test('the server\'s rules changing reach the session within a second, tighter or looser', async ($, on) => {
  const { clock, setRules } = await start($, on, { ...thread, on: false })
  const push = async () => ((await $.tool.call({ tool: 'Bash', command: 'git push --force origin tm/x' })) as { deny?: string }).deny
  expect(await push()).toBe(undefined)
  setRules(thread)
  // Held for a second: a burst of calls asks once.
  await clock.advance(500)
  expect(await push()).toBe(undefined)
  await clock.advance(501)
  expect(await push()).toContain('terminatr guard (force-push)')
  setRules({ ...thread, on: false })
  await clock.advance(1_001)
  expect(await push()).toBe(undefined)
})

test('a PowerShell command the guard can\'t read is asked, with its reason; one it refuses is refused', async ($, on) => {
  await start($, on, thread)
  const asked = await $.tool.check({ tool: 'PowerShell', input: { command: 'Invoke-Expression $line' } })
  expect([asked.decision, asked.reason]).toEqual(['ask', "terminatr guard: it can't tell what this PowerShell command runs (invoke-expression), so it asks first."])
  expect((await $.tool.check({ tool: 'PowerShell', input: { command: 'git status; Invoke-Expression $line; git branch -D x' } })).decision).toBe('deny')
  expect((await $.tool.check({ tool: 'PowerShell', input: { command: 'Get-ChildItem | Select-String foo' } })).decision).toBe('allow')
})

test('an excluded command that asks first is asked though Claude would allow it, and the server told first', async ($, on) => {
  const { escapes } = await start($, on, { ...thread, askFirst: ['npx playwright *'] })
  const input = { command: 'npx playwright test' }
  const asked = await $.tool.check({ tool: 'Bash', input })
  expect([asked.decision, asked.reason]).toEqual(['ask', 'terminatr guard: this command runs outside the sandbox (excluded command npx playwright *), and the user set it to ask first.'])
  const r = await $.tool.call({ tool: 'Bash', ...input })
  expect((r as { deny?: string }).deny).toBeUndefined()
  expect(escapes()).toBe(1)
  // Another command: nothing asked, nothing told.
  expect((await $.tool.check({ tool: 'Bash', input: { command: 'npx vitest' } })).decision).toBe('allow')
  await $.tool.call({ tool: 'Bash', command: 'npx vitest' })
  expect(escapes()).toBe(1)
})

test('a call that asks to leave the sandbox is asked, and the server told first; not under yolo', async ($, on) => {
  const { clock, escapes, setRules } = await start($, on, { ...thread, askEscapes: true })
  const input = { command: 'ls', dangerouslyDisableSandbox: true }
  const asked = await $.tool.check({ tool: 'Bash', input })
  expect([asked.decision, asked.reason]).toEqual(['ask', 'terminatr guard: this command asks to run outside the sandbox, so the user answers it.'])
  const r = await $.tool.call({ tool: 'Bash', ...input })
  expect((r as { deny?: string }).deny).toBeUndefined()
  expect(escapes()).toBe(1)
  // In the sandbox: nothing to tell.
  await $.tool.call({ tool: 'Bash', command: 'ls' })
  expect(escapes()).toBe(1)
  // Yolo: the floor only, nothing asked.
  setRules({ on: true, role: 'thread', home: '/h', cwd: WT, floor: ['force-push'], noWrite: ['/h/.terminatr/config.toml'], tools: TOOLS })
  await clock.advance(1_001)
  expect((await $.tool.check({ tool: 'Bash', input })).decision).toBe('allow')
  expect((await $.tool.check({ tool: 'Write', input: { file_path: '/h/.terminatr/config.toml' } })).decision).toBe('deny')
})

test('a write through a link out of the worktree is refused where it lands, and reported as given', async ($, on) => {
  const { clock, denied, resolved, setLinks } = await start($, on, thread)
  setLinks({ [`${WT}/link`]: '/etc' })
  const r = await $.tool.call({ tool: 'Write', file_path: 'link/hosts', content: 'x' })
  expect((r as { deny?: string }).deny).toContain('terminatr guard (worktree-only)')
  await clock.settle()
  expect(denied).toEqual([{ rule: 'worktree-only', tool: 'Write', summary: `Write of ${WT}/link/hosts`, subject: '/etc/hosts' }])
  expect(resolved).toEqual([[`${WT}/link/hosts`]])
  expect((await $.tool.check({ tool: 'Edit', input: { file_path: `${WT}/main.go` } })).decision).toBe('allow')
  expect((await $.tool.check({ tool: 'Edit', input: { file_path: `${WT}/link/passwd` } })).decision).toBe('deny')
  // A call that writes no file isn't asked about.
  expect((await $.tool.check({ tool: 'Read', input: { file_path: `${WT}/link/hosts` } })).decision).toBe('allow')
  expect(resolved.length).toBe(3)
})

test('a write the server can\'t place is refused, not judged as written', async ($, on) => {
  const { clock, denied, setLinks } = await start($, on, thread)
  setLinks(500)
  const r = await $.tool.call({ tool: 'Write', file_path: 'main.go', content: 'x' })
  expect((r as { deny?: string }).deny).toContain('could not be checked')
  expect((await $.tool.check({ tool: 'Edit', input: { file_path: 'main.go' } })).decision).toBe('deny')
  setLinks('hang')
  const late = $.tool.check({ tool: 'Edit', input: { file_path: 'main.go' } })
  await clock.advance(2_001)
  expect((await late).decision).toBe('deny')
  // Other calls go on; an unplaced write is no rule broken, so not reported.
  expect((await $.tool.check({ tool: 'Bash', input: { command: 'ls' } })).decision).toBe('allow')
  await clock.settle()
  expect(denied).toEqual([])
})

test('psCommands splits a PowerShell line', () => {
  expect(psCommands("cd x; $r = git status 2>&1 | Out-Null && & { gh pr view 1 } # git log\n<# c #> echo 'it''s' \"a $(gh pr view 2) b\"")).toEqual([
    ['cd', 'x'], ['$r', '=', 'git', 'status', '>'], ['Out-Null'], ['gh', 'pr', 'view', '1'], ['gh', 'pr', 'view', '2'], ['echo', "it's", 'a $(gh pr view 2) b'],
  ])
})
