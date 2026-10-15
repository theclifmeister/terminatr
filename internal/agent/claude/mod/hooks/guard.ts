// The guard (docs/SPEC.md §8.6, Guard): the standing rules as checks on
// each tool call. The server sends the rules in force for the session
// (GET /v1/rules on the mod socket, internal/server/guard.go); judge
// matches one call against them and names the rule it breaks, with the
// sentence the model reads. It refuses, or for a PowerShell command it
// can't read, asks: a call no rule matches goes on to Claude's own
// permission check, as without the mod.
//
// Bash commands are split into simple commands on ; & | && || newlines,
// ( ) and { }, the inside of a $( ) or backticks read as commands too,
// and read shell-style (quotes, comments, env assignments, wrappers such
// as sudo and timeout, bash -c, heredocs, git -C). That catches what
// agents type; the sandbox and the permission rules stay the backstop
// for what is written to hide.
// PowerShell commands (Claude's PowerShell tool on Windows, below) have
// no sandbox behind them, so what names its command by an expression, or
// evaluates a string, is put to the human (or auto mode's classifier).
// Plain functions of the rules and the call, for the tests.
//
// Agents without a mod get the same guard from its Go port
// (internal/guard), judged by the server on their PreToolUse hook. Both
// pass the shared vectors in tests/guard-vectors.ts: change them together.

// The rules as the server sends them (server.GuardRules).
export type Rules = {
  on: boolean
  role?: string
  rules?: readonly string[]
  home?: string
  cwd?: string
  writable?: readonly string[]
  // What git runs or reads as configuration in the main repo's git dir
  // (hooks, config, info): under worktree-only, no shell command names
  // them but to view them. The thread's sandbox refuses writing them
  // too; this says why sooner.
  gitConfig?: readonly string[]
  worktrees?: string
  protected?: readonly string[]
  secrets?: readonly string[]
  // The agent's tools the guard judges, by name (its manifest's
  // [guard.tools]); a tool not listed is not judged.
  tools?: Readonly<Record<string, Tool>>
  // The file system ignores case in names (macOS, Windows; plat/caps),
  // so paths are compared with case folded and ~/.Terminatr/worktrees
  // is the worktrees folder.
  caseFold?: boolean
  // The rules the user's enabled mods add: they only refuse, in force
  // whichever core rules the human turned off.
  mods?: readonly ModRule[]
}

// A guard rule a mod adds (mods design §7, Tighten only): the commands a
// shell tool may not run (each its words: the name, then words its
// arguments include in that order; a word ending in * matches any word
// it starts) and the files no file tool may write or read, with the
// mod's sentence. Judged on each simple command before the core's rules.
type ModRule = {
  mod: string
  id: string
  commands?: readonly (readonly string[])[]
  writes?: readonly string[]
  reads?: readonly string[]
  message: string
}

// What the guard knows of a tool: its kind (shell, write, patch, read,
// glob) and the fields of its input that kind reads. A shell tool's
// syntax is how its command lines are read: none for a POSIX shell,
// 'powershell' for PowerShell.
type Tool = { kind: string; syntax?: string; fields: readonly string[] }

// A refusal: the rule, what the model reads, and a short account for
// the journal that carries no text of the call beyond a path or branch.
// With ask it is no refusal but a question: the call is one the guard
// can't read (rule "unknown"), so it goes to Claude's dialog or
// classifier however its rules would allow it; message is the reason
// the dialog shows. An ask is not reported.
export type Denial = { rule: string; message: string; summary: string; ask?: boolean }

export const off: Rules = { on: false }

// rulesOf reads GET /v1/rules' body; anything else is the guard off.
export function rulesOf(text: string): Rules {
  try {
    const r = JSON.parse(text) as Rules
    return r && typeof r === 'object' && r.on === true && (Array.isArray(r.rules) || Array.isArray(r.mods)) ? r : off
  } catch {
    return off
  }
}

// judge is the rule the call of tool with input breaks, or null. The
// tool is judged by its kind in r.tools, from the fields listed there.
export function judge(r: Rules, tool: string, input: Record<string, unknown>): Denial | null {
  if (!r.on || (!r.rules?.length && !r.mods?.length)) return null
  const t = r.tools && Object.hasOwn(r.tools, tool) ? r.tools[tool] : undefined
  if (!t) return null
  const has = (id: string) => r.rules?.includes(id) ?? false
  const values = (t.fields ?? []).map(f => input[f]).filter((v): v is string => typeof v === 'string' && v !== '')
  const writable = (p: string) => (r.writable ?? []).some(w => under(r, p, w))
  // A file tool writing p (absolute).
  const judgeWrite = (p: string): Denial | null => {
    if (has('worktree-only') && !writable(p)) return deny(r, 'worktree-only', `${tool} of ${safe(p)}`)
    const m = (r.mods ?? []).find(m => (m.writes ?? []).some(w => under(r, p, w)))
    return m ? denyMod(m, `${tool} of ${safe(p)}`) : null
  }
  switch (t.kind) {
    case 'shell': {
      let ask: Denial | null = null
      for (const c of values) {
        const d = t.syntax === 'powershell' ? judgePowerShell(r, has, c) : judgeBash(r, has, c)
        if (d && !d.ask) return d
        ask ??= d
      }
      return ask
    }
    case 'write':
      for (const p of values) {
        const d = judgeWrite(abs(r, p))
        if (d) return d
      }
      return null
    case 'patch':
      // Each file the patch adds, updates, deletes or moves to is a write.
      for (const patch of values) {
        for (const p of patchPaths(patch)) {
          const d = judgeWrite(abs(r, p))
          if (d) return d
        }
      }
      return null
    case 'read':
    case 'glob':
      // A glob's fields may be patterns: judged by the part before the
      // first wildcard.
      for (const v of values) {
        const p = abs(r, v)
        if (has('credentials') && isSecret(r, p)) return deny(r, 'credentials', `${tool} of ${safe(p)}`)
        const m = (r.mods ?? []).find(m => covers(r, m.reads ?? [], p))
        if (m) return denyMod(m, `${tool} of ${safe(p)}`)
      }
      return null
  }
  return null
}

// patchPaths are the files a Codex patch (*** Begin Patch …) writes.
function patchPaths(patch: string): string[] {
  return [...patch.matchAll(/^\*\*\* (?:(?:Add|Update|Delete) File|Move to): (.+?)\s*$/gm)].map(m => m[1])
}

// The sentence the model reads for each rule; the end says what to do,
// which differs for a thread (its report) and the coordinator (the user).
function deny(r: Rules, rule: string, summary: string, target = ''): Denial {
  const tell = r.role === 'thread' ? 'say so in your report' : 'ask the user'
  const m: Record<string, string> = {
    'force-push': `force-pushing is not allowed here. Push without force; if the branch has diverged, merge instead of rewriting it, or ${tell}.`,
    'push-default': `nothing is pushed straight to ${target || 'the default branch'}: work lands through a pull request. Push your own branch and report; the coordinator opens the PR.`,
    'worktree-only': `a thread writes only inside its worktree (${r.writable?.[0] ?? ''}). Write the file there, or ${tell} what needs changing elsewhere.`,
    'delete-branch': `agents don't delete branches or worktrees; tm cleans them up once the work has merged. Leave them, and ${tell} if one is in the way.`,
    merge: `merging is the coordinator's job in this project (merge = "coordinator"). Leave the PR open with CI green and say so in your report.`,
    credentials: `credential files and tokens are not read here. If the task needs access you don't have, ${tell} exactly what is missing.`,
  }
  return { rule, message: `terminatr guard (${rule}): ${m[rule]}`, summary }
}

// judgeMods judges a simple command, its name and arguments, by the
// mods' rules; fold compares them with case folded (PowerShell's), and
// under caseFold the name is.
function judgeMods(r: Rules, name: string, args: readonly string[], fold: boolean): Denial | null {
  for (const m of r.mods ?? []) {
    for (const c of m.commands ?? []) {
      if (c.length === 0 || !modWord(c[0], name, fold || r.caseFold === true)) continue
      let j = 1
      for (const a of args) if (j < c.length && modWord(c[j], a, fold)) j++
      if (j === c.length) return denyMod(m, c.map(safe).join(' '))
    }
  }
  return null
}

// modWord: word w is pattern p, or starts with p's text before a final *.
function modWord(p: string, w: string, fold: boolean): boolean {
  if (fold) {
    p = p.toLowerCase()
    w = w.toLowerCase()
  }
  return p.endsWith('*') ? w.startsWith(p.slice(0, -1)) : p === w
}

// The refusal under mod rule m: its own sentence, labelled with the mod.
function denyMod(m: ModRule, summary: string): Denial {
  const rule = `mod:${m.mod}.${m.id}`
  return { rule, message: `terminatr guard (${rule}): ${m.message}`, summary }
}

// judgeBash checks each simple command of a Bash command line.
function judgeBash(r: Rules, has: (id: string) => boolean, command: string): Denial | null {
  for (const words of commands(command)) {
    const argv = strip(words)
    if (argv.length === 0) continue
    const cmd = base(argv[0])
    const args = argv.slice(1)
    const md = judgeMods(r, cmd, args, false)
    if (md) return md
    const gd = judgeGitConfig(r, has, cmd, words)
    if (gd) return gd
    if (cmd === 'git') {
      const d = judgeGit(r, has, args)
      if (d) return d
    } else if (cmd === 'gh') {
      const d = judgeGh(r, has, args)
      if (d) return d
    } else if (cmd === 'az') {
      const d = judgeAz(r, has, args)
      if (d) return d
    } else if (HTTP_CLIENTS.has(cmd)) {
      const d = judgeHttp(r, has, cmd, args)
      if (d) return d
    } else if (SHELLS.has(cmd)) {
      // bash -c STRING runs the string.
      const i = args.findIndex(a => SHELL_C.test(a))
      const d = i >= 0 && i + 1 < args.length ? judgeBash(r, has, args[i + 1]) : null
      if (d) return d
    } else if (cmd === 'eval') {
      const d = judgeBash(r, has, args.join(' '))
      if (d) return d
    } else if (cmd === 'pwsh' || cmd === 'powershell') {
      const d = judgePwsh(r, has, args)
      if (d) return d
    } else if (cmd === 'rm' && has('delete-branch') && args.some(a => /^-[a-zA-Z]*[rR]/.test(a) || a === '--recursive')) {
      for (const a of args) {
        if (a.startsWith('-')) continue
        if (isWorktreeRoot(r, abs(r, a))) return deny(r, 'delete-branch', `rm -r of ${safe(abs(r, a))}`)
      }
    }
    if (has('credentials')) {
      const d = judgeSecret(r, cmd, args, words, READERS.has(cmd), PRINTERS.has(cmd))
      if (d) return d
    }
  }
  return null
}

// SHELLS run a command line given with -c; STDIN_SHELLS (them and more)
// run the commands a heredoc gives them.
const SHELLS = new Set('bash sh zsh dash ksh mksh ash'.split(' '))
const STDIN_SHELLS = new Set('bash sh zsh dash ksh mksh ash fish csh tcsh pwsh powershell source . eval xargs ssh sudo doas su'.split(' '))
const SHELL_C = /^-[a-zA-Z]*c[a-zA-Z]*$/

// The commands that only show the files they are given.
const VIEWERS = new Set('cat head tail less more bat nl grep egrep fgrep rg ls stat file wc diff cmp'.split(' '))

// judgeGitConfig: under worktree-only, cmd naming the main repo's git
// hooks, config or info other than to view them, or redirecting into
// them.
function judgeGitConfig(r: Rules, has: (id: string) => boolean, cmd: string, words: string[]): Denial | null {
  if (!has('worktree-only') || !r.gitConfig?.length) return null
  for (let i = 0; i < words.length; i++) {
    const w = words[i]
    if (w === '<' || w === '>' || w.startsWith('-')) continue
    const out = i > 0 && words[i - 1] === '>'
    const p = abs(r, w)
    if (r.gitConfig.some(g => under(r, p, g)) && (out || !VIEWERS.has(cmd))) return deny(r, 'worktree-only', `${cmd} of ${safe(p)}`)
  }
  return null
}

function judgeGit(r: Rules, has: (id: string) => boolean, args: string[]): Denial | null {
  // Global options before the subcommand.
  let i = 0
  while (i < args.length && args[i].startsWith('-')) {
    if (args[i] === '-C' || args[i] === '-c' || args[i] === '--git-dir' || args[i] === '--work-tree' || args[i] === '--namespace') i++
    i++
  }
  const sub = args[i]
  const rest = args.slice(i + 1)
  // long: one of the long options, with or without a value; flag: that
  // or the short one, alone or among others (-fu; digits too: -4 is
  // --ipv4).
  const long = (...names: string[]) => rest.some(a => names.some(l => a === l || a.startsWith(l + '=')))
  const flag = (short: string, ...names: string[]) => long(...names) || rest.some(a => /^-[a-zA-Z0-9]+$/.test(a) && a.includes(short))
  switch (sub) {
    case 'push': {
      if (has('force-push') && (flag('f', '--force', '--force-with-lease', '--force-if-includes', '--mirror') || rest.some(a => a.startsWith('+')))) {
        return deny(r, 'force-push', 'git push with force')
      }
      if (has('delete-branch') && (flag('d', '--delete', '--prune') || rest.some(a => a.startsWith(':') && a.length > 1))) {
        return deny(r, 'delete-branch', 'git push deleting a branch')
      }
      if (has('push-default')) {
        if (long('--all', '--branches', '--mirror')) return deny(r, 'push-default', 'git push --all')
        const pos = positionals(rest, ['-o', '--push-option', '--repo', '--receive-pack', '--exec'])
        for (const spec of pos.slice(1)) {
          const target = branchOf(spec.includes(':') ? spec.slice(spec.indexOf(':') + 1) : spec)
          if (target && (r.protected ?? []).includes(target)) return deny(r, 'push-default', `git push to ${safe(target)}`, target)
        }
      }
      return null
    }
    case 'branch':
      if (has('delete-branch') && flag('d', '--delete')) return deny(r, 'delete-branch', 'git branch --delete')
      if (has('delete-branch') && rest.some(a => /^-[a-zA-Z0-9]*D/.test(a))) return deny(r, 'delete-branch', 'git branch -D')
      return null
    case 'worktree':
      if (has('delete-branch') && (rest[0] === 'remove' || rest[0] === 'prune')) return deny(r, 'delete-branch', `git worktree ${rest[0]}`)
      return null
    case 'update-ref':
      if (has('delete-branch') && flag('d', '--delete')) return deny(r, 'delete-branch', 'git update-ref -d')
      return null
    case 'credential':
      if (has('credentials') && rest[0] === 'fill') return deny(r, 'credentials', 'git credential fill')
      return null
  }
  return null
}

// gh's options that take a value, which may come before or between the
// command words (gh pr -R o/r merge).
const GH_VALUED = new Set(['-R', '--repo', '--hostname'])

function judgeGh(r: Rules, has: (id: string) => boolean, args: string[]): Denial | null {
  // The command words, flags among them aside.
  const words: string[] = []
  let api = -1
  for (let i = 0; i < args.length && words.length < 2; i++) {
    if (args[i].startsWith('-')) {
      if (GH_VALUED.has(args[i])) i++
      continue
    }
    if (words.length === 0 && args[i] === 'api') api = i
    words.push(args[i])
  }
  const [a, b] = words
  if (a === 'pr' && b === 'merge' && has('merge')) return deny(r, 'merge', 'gh pr merge')
  if (a === 'repo' && b === 'delete' && has('delete-branch')) return deny(r, 'delete-branch', 'gh repo delete')
  if (a === 'auth' && has('credentials') && (b === 'token' || args.includes('--show-token') || args.includes('-t'))) {
    return deny(r, 'credentials', `gh auth ${b === 'token' ? 'token' : 'status --show-token'}`)
  }
  if (a === 'api') {
    // Each path without its query or fragment, which GitHub ignores.
    const paths = args.slice(api + 1).filter(x => !x.startsWith('-')).map(x => x.split(/[?#]/)[0])
    if (has('merge') && paths.some(p => /pulls\/[^/]+\/merge\/?$/.test(p))) return deny(r, 'merge', 'gh api merging a PR')
    if (paths.some(p => p.replace(/^\/+|\/+$/g, '') === 'graphql')) {
      // The GraphQL mutations that merge a PR, and that delete a branch.
      if (has('merge') && args.some(x => /mergePullRequest|enablePullRequestAutoMerge|enqueuePullRequest/i.test(x))) return deny(r, 'merge', 'gh api graphql merging a PR')
      if (has('delete-branch') && args.some(x => /deleteRef/i.test(x))) return deny(r, 'delete-branch', 'gh api graphql deleting a branch')
    }
    const method = args.find((x, i) => i > 0 && (args[i - 1] === '-X' || args[i - 1] === '--method'))?.toUpperCase()
      ?? args.find(x => /^(-X|--method=)/.test(x) && x.length > 2)?.replace(/^(-X|--method=)/, '').toUpperCase()
    if (has('delete-branch') && method === 'DELETE' && paths.some(p => /git\/refs\/heads\//.test(p))) return deny(r, 'delete-branch', 'gh api deleting a branch')
  }
  return null
}

// az (the Azure CLI with its azure-devops extension) is judged like gh,
// with the same rules and sentences. The extension's verbs: completing a
// PR (`az repos pr update --status completed`, `--auto-complete`,
// `--bypass-policy`, also on create) is the merge; `--delete-source-branch`,
// `az repos delete` and `az repos ref delete` delete; the REST routes under
// az rest, az devops invoke and curl are held to the same, by method and
// route.
function judgeAz(r: Rules, has: (id: string) => boolean, args: string[]): Denial | null {
  // The command words come first; flags (any order, --x=v or --x v) after.
  let i = 0
  while (i < args.length && args[i].startsWith('-')) i += optionTakesValue(args[i]) ? 2 : 1
  const words: string[] = []
  for (; i < args.length && !args[i].startsWith('-'); i++) words.push(args[i].toLowerCase())
  const rest = args.slice(i)
  const [a, b, c] = words
  if (a === 'repos' && b === 'pr' && (c === 'update' || c === 'create')) {
    const completes = opts(rest, '--status').some(v => v.toLowerCase() === 'completed') ||
      truthy(rest, '--auto-complete') || truthy(rest, '--bypass-policy')
    if (has('merge') && completes) return deny(r, 'merge', `az repos pr ${c} completing a PR`)
    if (has('delete-branch') && truthy(rest, '--delete-source-branch')) return deny(r, 'delete-branch', `az repos pr ${c} --delete-source-branch`)
  }
  if (a === 'repos' && b === 'delete' && has('delete-branch')) return deny(r, 'delete-branch', 'az repos delete')
  if (a === 'repos' && b === 'ref' && c === 'delete' && has('delete-branch')) return deny(r, 'delete-branch', 'az repos ref delete')
  if (has('credentials')) {
    if (a === 'account' && b === 'get-access-token') return deny(r, 'credentials', 'az account get-access-token')
    if (a === 'devops' && b === 'login') return deny(r, 'credentials', 'az devops login')
  }
  if (a === 'rest') {
    // A method override header is the method the service acts on.
    const override = optsMulti(rest, '--headers').map(h => /^x-http-method-override[=:]\s*(\w+)/i.exec(h)?.[1]).find(Boolean)
    const method = override ?? opts(rest, '--method', '-m')[0] ?? 'GET'
    return judgeRoute(r, has, method, opts(rest, '--url', '--uri', '-u')[0] ?? '')
  }
  if (a === 'devops' && b === 'invoke') {
    const resource = (opts(rest, '--resource')[0] ?? '').toLowerCase()
    const method = opts(rest, '--http-method')[0] ?? 'GET'
    const params = optsMulti(rest, '--route-parameters').map(p => p.toLowerCase())
    const area = (opts(rest, '--area')[0] ?? 'git').toLowerCase()
    if (area !== 'git') return null
    if (!isWrite(method)) return null
    if (has('merge') && (resource === 'merges' || (resource === 'pullrequests' && params.some(p => p.startsWith('pullrequestid='))))) {
      return deny(r, 'merge', `az devops invoke ${safe(method.toUpperCase())} on ${safe(resource)}`)
    }
    if (has('delete-branch') && (resource === 'refs' || (resource === 'repositories' && method.toUpperCase() === 'DELETE'))) {
      return deny(r, 'delete-branch', `az devops invoke ${safe(method.toUpperCase())} on ${safe(resource)}`)
    }
  }
  return null
}

// Clients that speak HTTP; given an Azure DevOps address they are judged
// by route and method as az rest is, and by what they authenticate with.
const HTTP_CLIENTS = new Set(['curl', 'wget', 'http', 'https', 'xh'])

const AZURE_HOST = /(^|[/@.])(dev\.azure\.com|[a-z0-9-]+\.visualstudio\.com|vssps\.dev\.azure\.com)([/:?#]|$)/i

function judgeHttp(r: Rules, has: (id: string) => boolean, cmd: string, args: string[]): Denial | null {
  const i = args.findIndex(a => AZURE_HOST.test(a))
  if (i < 0) return null
  if (has('credentials') && args.some(a => /^(-u.*|--user(=.*)?|--oauth2-bearer(=.*)?)$/.test(a) || /authorization:|AZURE_DEVOPS_EXT_PAT/i.test(a))) {
    return deny(r, 'credentials', 'an HTTP call to Azure DevOps with credentials')
  }
  return judgeRoute(r, has, httpMethod(cmd, args, i), args[i])
}

// curl's short options that take a value; the methods HTTPie and xh take
// as a word before the URL; an HTTPie request item that sends data (a=b,
// a:=1, a@file), not a query (a==b) or a header (A:b).
const CURL_VALUED = 'AbcCdDeEFHKmoPQrtTuUwxXyYz'
const HTTP_METHODS = new Set('GET POST PUT PATCH DELETE HEAD OPTIONS'.split(' '))
const HTTPIE_DATA = /^[^=:@]+(?:=[^=]|=$|:=|@)/

// httpMethod is the method an HTTP client's arguments send, the URL at
// args[url]: the one they name, else what the data they send makes it
// (curl -d is a POST, -T a PUT), else GET.
function httpMethod(cmd: string, args: string[], url: number): string {
  let method = ''
  let implied = ''
  const imply = (m: string) => {
    if (implied !== 'PUT') implied = m
  }
  for (let i = 0; i < args.length; i++) {
    const a = args[i]
    if (a === '-X' || a === '--request') method = args[i + 1] ?? ''
    else if (a.startsWith('--request=')) method = a.slice(10)
    else if (/^-X./.test(a)) method = a.slice(2)
  }
  const cut = (a: string): [string, string, boolean] => {
    const k = a.indexOf('=')
    return k < 0 ? [a, '', false] : [a.slice(0, k), a.slice(k + 1), true]
  }
  if (cmd === 'curl') {
    let get = false
    for (let i = 0; i < args.length; i++) {
      const a = args[i]
      if (a.startsWith('--')) {
        let [name, val, eq] = cut(a)
        if (name === '--request') {
          if (!eq) val = args[++i] ?? ''
          method = val
        } else if (name === '--get') get = true
        else if (name === '--upload-file') imply('PUT')
        else if (name.startsWith('--data') || name === '--json' || name === '--form' || name === '--form-string') imply('POST')
        continue
      }
      if (!a.startsWith('-')) continue
      // A bundle of short options (-sSd @b.json): the first that takes a
      // value takes the rest of the word, or the next one.
      for (let k = 1; k < a.length; k++) {
        const c = a[k]
        let rest = a.slice(k + 1)
        if (c === 'G') {
          get = true
          continue
        }
        if (c === 'd' || c === 'F') imply('POST')
        else if (c === 'T') imply('PUT')
        else if (!CURL_VALUED.includes(c)) continue
        if (rest === '') rest = args[++i] ?? ''
        if (c === 'X') method = rest
        break
      }
    }
    if (get && implied === 'POST') implied = ''
  } else if (cmd === 'wget') {
    for (let i = 0; i < args.length; i++) {
      const [name, val, eq] = cut(args[i])
      if (name === '--method') method = eq ? val : (args[i + 1] ?? '')
      else if (name === '--post-data' || name === '--post-file') imply('POST')
    }
  } else if (cmd === 'http' || cmd === 'https' || cmd === 'xh') {
    if (url > 0 && HTTP_METHODS.has(args[url - 1].toUpperCase())) method = args[url - 1]
    if (args.some(a => a === '-f' || a === '--form' || a === '--multipart' || a === '--raw' || a.startsWith('--raw='))) imply('POST')
    if (args.slice(url + 1).some(a => !a.startsWith('-') && HTTPIE_DATA.test(a))) imply('POST')
  }
  return method || implied || 'GET'
}

// judgeRoute: a write method on an Azure DevOps Git route that completes
// a PR or deletes a ref or a repository.
function judgeRoute(r: Rules, has: (id: string) => boolean, method: string, url: string): Denial | null {
  const m = method.toUpperCase()
  if (!isWrite(m)) return null
  const path = url.split(/[?#]/)[0].toLowerCase().replace(/\/+$/, '')
  if (!path.includes('/_apis/git/')) return null
  if (has('merge') && (/\/pullrequests\/[^/]+$/.test(path) || /\/merges$/.test(path))) {
    return deny(r, 'merge', `${safe(m)} on an Azure DevOps pull request`)
  }
  if (has('delete-branch') && (/\/refs$/.test(path) || (m === 'DELETE' && /\/repositories\/[^/]+$/.test(path)))) {
    return deny(r, 'delete-branch', `${safe(m)} on an Azure DevOps ref or repository`)
  }
  return null
}

function isWrite(method: string): boolean {
  return !['GET', 'HEAD', 'OPTIONS', ''].includes(method.toUpperCase())
}

// az's global options that take a value before the command words.
function optionTakesValue(a: string): boolean {
  return ['--output', '-o', '--query', '--subscription', '--organization', '--org', '--project', '-p'].includes(a)
}

// opts: the values of the option under any of names, as --x v or --x=v.
function opts(args: string[], ...names: string[]): string[] {
  const out: string[] = []
  for (let i = 0; i < args.length; i++) {
    for (const n of names) {
      if (args[i] === n) out.push(args[i + 1] !== undefined && !args[i + 1].startsWith('-') ? args[i + 1] : '')
      else if (args[i].startsWith(n + '=')) out.push(args[i].slice(n.length + 1))
    }
  }
  return out
}

// optsMulti: every value after the option up to the next option, for
// those that take several (--route-parameters a=1 b=2).
function optsMulti(args: string[], name: string): string[] {
  const out: string[] = []
  for (let i = 0; i < args.length; i++) {
    if (args[i] === name) {
      for (let j = i + 1; j < args.length && !args[j].startsWith('-'); j++) out.push(args[j])
    } else if (args[i].startsWith(name + '=')) out.push(args[i].slice(name.length + 1))
  }
  return out
}

// truthy: a boolean option given true, or bare (az takes true/false, and
// a bare flag counts as given).
function truthy(args: string[], name: string): boolean {
  return opts(args, name).some(v => !['false', 'no', 'n', '0', 'off'].includes(v.toLowerCase()))
}

// Commands that print their arguments; ENV_PRINTERS print the variables
// when given no names.
const PRINTERS = new Set(['echo', 'printf', 'print'])
const ENV_PRINTERS = new Set(['export', 'declare', 'typeset'])

// Commands that print or copy what they are given.
const READERS = new Set([
  'cat', 'head', 'tail', 'less', 'more', 'bat', 'nl', 'tac', 'cp', 'mv', 'scp', 'rsync', 'base64', 'xxd', 'od',
  'hexdump', 'strings', 'grep', 'egrep', 'fgrep', 'rg', 'ag', 'awk', 'sed', 'cut', 'sort', 'uniq', 'tar', 'zip',
  'gzip', 'openssl', 'gpg', 'jq', 'yq', 'diff', 'cmp', 'source', '.', 'curl', 'dd',
])

// Environment variable names that look like they hold a secret.
const SECRET_NAME = /token|secret|key|password|passwd|credential|(^|_)pat$/i

// judgeSecret: cmd (a reader or a printer of what it is given) reading a
// secret or printing the environment.
function judgeSecret(r: Rules, cmd: string, args: string[], words: string[], reader: boolean, printer: boolean): Denial | null {
  if (cmd === 'security' && /^(find-(generic|internet)-password|dump-keychain|export)$/.test(args[0] ?? '')) {
    return deny(r, 'credentials', `security ${args[0]}`)
  }
  if (cmd === 'printenv') {
    // printenv NAME... prints only what it is asked for: fine unless a
    // name looks secret. With no name (or only substitutions) it prints
    // everything.
    const names = unredirected(args).filter(a => !a.startsWith('-') && !substituted(a))
    if (!names.length) return deny(r, 'credentials', 'printenv printing the environment')
    const secret = names.find(n => SECRET_NAME.test(n))
    if (secret) return deny(r, 'credentials', `printenv ${safe(secret)}`)
  } else if (cmd === 'env' && unredirected(args).every(a => a.startsWith('-') || substituted(a))) {
    return deny(r, 'credentials', 'env printing the environment')
  } else if ((ENV_PRINTERS.has(cmd) && args.every(a => a.startsWith('-') && !/[fF]/.test(a))) || (cmd === 'set' && args.length === 0)) {
    // export, declare -x and the like with no names print the variables
    // (with -f, the functions); set alone prints them all.
    return deny(r, 'credentials', `${cmd} printing the environment`)
  }
  // echo and the like given a secret variable ($AZURE_DEVOPS_EXT_PAT).
  if (printer || reader) {
    for (const w of words) {
      const v = patVar(w)
      if (v) return deny(r, 'credentials', `${cmd} of ${safe(v)}`)
    }
  }
  // A reader given a secret, or a secret redirected in.
  for (let i = 0; i < words.length; i++) {
    const w = words[i]
    const redirected = w === '<' || words[i - 1] === '<'
    const path = w.startsWith('<') && w.length > 1 ? w.slice(1) : w
    if ((reader || redirected) && !path.startsWith('-') && path !== '<' && isSecret(r, abs(r, path))) {
      return deny(r, 'credentials', `${cmd} of ${safe(abs(r, path))}`)
    }
  }
  return null
}

// unredirected is args without their redirections (> f, < f, << EOF).
function unredirected(args: string[]): string[] {
  const out: string[] = []
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '<' || args[i] === '>') {
      while (args[i + 1] === '<' || args[i + 1] === '>') i++
      i++
      continue
    }
    out.push(args[i])
  }
  return out
}

// substituted: word a is a substitution alone, $( ), backticks or a
// PowerShell expression, which may give no word at all.
function substituted(a: string): boolean {
  return a.startsWith('$(') || a.startsWith('`') || psExprArg(a)
}

// patVar is the Azure DevOps token variable w names ($AZURE_DEVOPS_EXT_PAT,
// $env:AZURE_DEVOPS_EXT_PAT), or ''.
function patVar(w: string): string {
  for (const m of w.matchAll(/\$\{?(?:[Ee][Nn][Vv]:)?([A-Za-z_][A-Za-z0-9_]*)/g)) {
    if (/^AZURE_DEVOPS_EXT_PAT$|^AZURE_DEVOPS_.*(PAT|TOKEN)$/i.test(m[1])) return m[1]
  }
  return ''
}

// A here-document a command line opens (<<WORD, <<-WORD): the word that
// ends it, whether its lines lose their leading tabs, and whether the
// word was quoted (then nothing in it expands); then its body once read.
type Heredoc = { delim: string; tabs: boolean; quoted: boolean; body: string }

// commands splits a command line into simple commands, each a list of
// words with quotes removed; redirections stay words ("<", ">"). A $( )
// or backtick is a word of its command, its inside read as commands too.
// A heredoc's body is read as commands when the line has a shell to run
// it; otherwise only what it substitutes is. In arithmetic (arith) << is
// a shift, no heredoc.
export function commands(line: string, arith = false): string[][] {
  const out: string[][] = []
  let inner: string[][] = []
  let words: string[] = []
  // word is the word as written; lit the same without its unquoted
  // substitutions, what the word is when they print nothing (printenv$(x)
  // is printenv): the word, unless it is substitutions alone.
  let word = ''
  let lit = ''
  let inWord = false
  let hasLit = false
  let open: Heredoc[] = []
  const docs: Heredoc[] = []
  const endWord = () => {
    if (inWord) words.push(hasLit ? lit : word)
    word = ''
    lit = ''
    inWord = false
    hasLit = false
  }
  // text is literal text of the word.
  const text = (t: string) => {
    word += t
    lit += t
    inWord = true
    hasLit = true
  }
  const endCommand = () => {
    endWord()
    if (words.length) out.push(words)
    // What the command substitutes, after it.
    out.push(...inner)
    words = []
    inner = []
  }
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (c === '\\' && i + 1 < line.length) {
      if (line[i + 1] !== '\n') text(line[i + 1])
      i++
    } else if (c === "'") {
      const j = line.indexOf("'", i + 1)
      const end = j < 0 ? line.length : j
      text(line.slice(i + 1, end))
      i = end
    } else if (c === '"') {
      // Inside double quotes a $( ) or backtick still runs: its inside
      // is read as commands too.
      let j = i + 1
      let s = ''
      while (j < line.length && line[j] !== '"') {
        if (line[j] === '\\' && j + 1 < line.length) {
          s += line[j + 1]
          j += 2
        } else if (line[j] === '$' && line[j + 1] === '(') {
          const end = closing(line, j + 2, '(', ')')
          out.push(...commands(line.slice(j + 2, end)))
          s += line.slice(j, end + 1)
          j = end + 1
        } else if (line[j] === '`') {
          const end = tickClose(line, j + 1)
          out.push(...commands(line.slice(j + 1, end)))
          s += line.slice(j, end + 1)
          j = end + 1
        } else s += line[j++]
      }
      text(s)
      i = j
    } else if (c === ' ' || c === '\t') {
      endWord()
    } else if (c === '#' && !inWord) {
      // A comment, to the end of the line.
      while (i + 1 < line.length && line[i + 1] !== '\n') i++
    } else if (c === '(' && line[i + 1] === '(') {
      // Arithmetic, (( … )), whose << is a shift, not a heredoc: read on
      // its own, as commands (bash takes ((a; b)) for subshells).
      const end = closing(line, i + 1, '(', ')')
      endCommand()
      out.push(...commands(line.slice(i + 2, end), true))
      i = end
    } else if (c === '$' && line[i + 1] === '[' && closing(line, i + 2, '[', ']') < line.length) {
      // Arithmetic, $[ … ], a part of its word: its inside read on its
      // own, as commands.
      const end = closing(line, i + 2, '[', ']')
      inner.push(...commands(line.slice(i + 2, end), true))
      text(line.slice(i, end + 1))
      i = end
    } else if (c === '$' && line[i + 1] === '{' && i + 2 < line.length && !' \t\n|'.includes(line[i + 2])) {
      // A parameter expansion, ${NAME} or ${NAME:-word}, is part of the
      // word (${ cmd; } runs commands: read below as { }). A $( ) or
      // backtick in it still runs.
      const end = closing(line, i + 2, '{', '}')
      for (const s of substitutions(line.slice(i + 2, end))) out.push(...commands(s))
      text(line.slice(i, end + 1))
      i = end
    } else if (c === '$' && line[i + 1] === '(') {
      // A command substitution is a word of the command it is in.
      const end = closing(line, i + 2, '(', ')')
      inner.push(...commands(line.slice(i + 2, end)))
      word += line.slice(i, end + 1)
      inWord = true
      i = end
    } else if (c === '`') {
      const end = tickClose(line, i + 1)
      inner.push(...commands(line.slice(i + 1, end)))
      word += line.slice(i, end + 1)
      inWord = true
      i = end
    } else if (c === '\n' && open.length) {
      // The bodies of the heredocs the line opened.
      endCommand()
      let j = i + 1
      for (const h of open) {
        let body = ''
        while (j < line.length) {
          let k = line.indexOf('\n', j)
          if (k < 0) k = line.length
          let l = line.slice(j, k)
          if (h.tabs) l = l.replace(/^\t+/, '')
          j = k + 1
          if (l === h.delim) break
          body += l + '\n'
        }
        docs.push({ ...h, body })
      }
      open = []
      i = j - 1
    } else if (c === ';' || c === '&' || c === '|' || c === '\n' || c === '(' || c === ')' || c === '{' || c === '}') {
      endCommand()
    } else if (c === '<' && !arith && line[i + 1] === '<' && line[i + 2] !== '<' && line[i - 1] !== '<') {
      // A heredoc: <<WORD or <<-WORD.
      endWord()
      words.push('<', '<')
      let j = i + 2
      const h: Heredoc = { delim: '', tabs: false, quoted: false, body: '' }
      if (line[j] === '-') {
        h.tabs = true
        j++
      }
      while (line[j] === ' ' || line[j] === '\t') j++
      while (j < line.length && !' \t\n;&|<>()'.includes(line[j])) {
        const q = line[j]
        if (q === "'" || q === '"') {
          h.quoted = true
          let end = line.indexOf(q, j + 1)
          if (end < 0) end = line.length
          h.delim += line.slice(j + 1, end)
          j = end + 1
        } else if (q === '\\') {
          h.quoted = true
          h.delim += line[j + 1] ?? ''
          j += 2
        } else {
          h.delim += q
          j++
        }
      }
      if (h.delim !== '' || h.quoted) {
        words.push(h.delim)
        open.push(h)
      }
      i = j - 1
    } else if (c === '<' || c === '>') {
      endWord()
      words.push(c)
    } else text(c)
  }
  endCommand()
  if (docs.length) {
    // A shell anywhere on the line (ssh host bash, docker exec -i c sh)
    // may run what a heredoc gives it.
    const shell = out.some(w => {
      const argv = strip(w)
      return (argv.length > 0 && STDIN_SHELLS.has(base(argv[0]))) || w.some(a => SHELLS.has(base(a)))
    })
    for (const h of docs) {
      // The line's own shell expands an unquoted body first.
      if (!h.quoted) for (const s of substitutions(h.body)) out.push(...commands(s))
      if (shell) out.push(...commands(h.body))
    }
  }
  return out
}

// substitutions are the insides of the $( ) and backticks in s, which run
// where s is expanded.
function substitutions(s: string): string[] {
  const out: string[] = []
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '\\') i++
    else if (s[i] === '$' && s[i + 1] === '(') {
      const end = closing(s, i + 2, '(', ')')
      out.push(s.slice(i + 2, end))
      i = end
    } else if (s[i] === '`') {
      const end = tickClose(s, i + 1)
      out.push(s.slice(i + 1, end))
      i = end
    }
  }
  return out
}

// closing is the index of the close that closes an open before i ({ },
// ( )), skipping quoted strings, backticks and escapes, or s.length.
function closing(s: string, i: number, open: string, close: string): number {
  let depth = 1
  for (; i < s.length; i++) {
    const c = s[i]
    if (c === '\\') i++
    else if (c === "'") {
      const j = s.indexOf("'", i + 1)
      if (j < 0) return s.length
      i = j
    } else if (c === '"') {
      for (i++; i < s.length && s[i] !== '"'; i++) {
        if (s[i] === '\\') i++
        else if (s[i] === '$' && s[i + 1] === '(') i = closing(s, i + 2, '(', ')')
        else if (s[i] === '`') i = tickClose(s, i + 1)
      }
    } else if (c === '`') i = tickClose(s, i + 1)
    else if (c === open) depth++
    else if (c === close) {
      if (--depth === 0) return i
    }
  }
  return s.length
}

// tickClose is the index of the backtick that closes one opened before
// i, or s.length.
function tickClose(s: string, i: number): number {
  for (; i < s.length; i++) {
    if (s[i] === '\\') i++
    else if (s[i] === '`') return i
  }
  return s.length
}

// How a command that runs another reads its own options: the short ones
// that take a value (-n 5, -n5), the long ones that do (--x v; --x=v is
// one word), and how many words come before the command (timeout's
// duration). The shell's keywords come before a command too.
type Wrapper = { short?: string; long?: string[]; skip?: number }
const WRAPPERS: Record<string, Wrapper> = {
  sudo: { short: 'ughpCDrtTU', long: ['--user', '--group', '--host', '--prompt', '--close-from', '--chdir', '--role', '--type', '--command-timeout', '--other-user'] },
  doas: { short: 'uC' },
  env: { short: 'uCSP', long: ['--unset', '--chdir', '--split-string'] },
  timeout: { short: 'sk', long: ['--signal', '--kill-after'], skip: 1 },
  nice: { short: 'n', long: ['--adjustment'] },
  ionice: { short: 'cnpPu', long: ['--class', '--classdata', '--pid', '--pgid', '--uid'] },
  xargs: { short: 'ILnPsdEa', long: ['--arg-file', '--delimiter', '--max-args', '--max-procs', '--max-chars', '--max-lines', '--process-slot-var'] },
  stdbuf: { short: 'ioe', long: ['--input', '--output', '--error'] },
  time: { short: 'fo', long: ['--format', '--output'] },
  exec: { short: 'a' },
  caffeinate: { short: 'tw' },
  command: {}, builtin: {}, nohup: {}, unbuffer: {}, setsid: {}, chronic: {},
  if: {}, then: {}, else: {}, elif: {}, while: {}, until: {}, do: {}, '!': {},
}

const ASSIGNMENT = /^[A-Za-z_][A-Za-z0-9_]*=/

// strip drops what runs a command without being it: env assignments, the
// wrappers (sudo, env, timeout 5, xargs …) with their options, and the
// keywords before a command (if, then, do …), and redirections before
// it (> f git …). A wrapper with no command after it is the command (env
// alone prints the environment).
function strip(words: string[]): string[] {
  let i = 0
  let from = -1
  while (i < words.length) {
    const w = words[i]
    if (ASSIGNMENT.test(w)) {
      i++
      continue
    }
    if (w === '<' || w === '>') {
      while (words[i + 1] === '<' || words[i + 1] === '>') i++
      i += 2
      continue
    }
    const wr = Object.hasOwn(WRAPPERS, w) ? WRAPPERS[w] : undefined
    if (!wr) return words.slice(i)
    from = i
    i++
    while (i < words.length && words[i].startsWith('-') && words[i] !== '-') {
      const o = words[i++]
      if (o === '--') break
      let name = o
      let val = ''
      if (o.startsWith('--')) {
        const k = o.indexOf('=')
        if (k >= 0) {
          name = o.slice(0, k)
          val = o.slice(k + 1)
        } else if ((wr.long ?? []).includes(o) && i < words.length) val = words[i++]
      } else {
        let k = 1
        while (k < o.length && !(wr.short ?? '').includes(o[k])) k++
        if (k < o.length) {
          name = '-' + o[k]
          val = o.slice(k + 1)
          if (val === '' && i < words.length) val = words[i++]
        }
      }
      if (w === 'env' && (name === '-S' || name === '--split-string')) {
        // env -S 'a b': the string is words.
        words = [...val.split(/\s+/).filter(Boolean), ...words.slice(i)]
        from = 0
        i = 0
        break
      }
    }
    if (w === 'env') while (i < words.length && ASSIGNMENT.test(words[i])) i++
    i += wr.skip ?? 0
  }
  return from >= 0 && from < words.length ? words.slice(from) : []
}

// positionals are args without their options, valued options skipping
// their value.
function positionals(args: string[], valued: string[]): string[] {
  const out: string[] = []
  for (let i = 0; i < args.length; i++) {
    if (valued.includes(args[i])) i++
    else if (args[i] === '<' || args[i] === '>') i++
    else if (!args[i].startsWith('-')) out.push(args[i])
  }
  return out
}

function branchOf(ref: string): string {
  return ref.replace(/^\+/, '').replace(/^refs\/heads\//, '')
}

function base(p: string): string {
  return p.slice(p.lastIndexOf('/') + 1)
}

// What a path may start with for the home directory, case aside: ~,
// $HOME, and PowerShell's $env:USERPROFILE and $env:HOME.
const HOMES = ['~', '$home', '${home}', '$env:userprofile', '${env:userprofile}', '$env:home', '${env:home}']

// A Windows path from its drive's root (C:/, or C: alone).
const DRIVE = /^[A-Za-z]:(\/|$)/

// slash is p with Windows' backslashes as slashes.
function slash(p: string): string {
  return p.replaceAll('\\', '/')
}

// abs is p made absolute and clean: ~ is home, a relative path is from
// the session's folder. A Windows path keeps its drive (C:/Users/me);
// backslashes are slashes.
function abs(r: Rules, p: string): string {
  p = slash(p)
  const h = HOMES.find(h => p.slice(0, h.length).toLowerCase() === h && (p.length === h.length || p[h.length] === '/'))
  if (h !== undefined) p = slash(r.home ?? '') + p.slice(h.length)
  if (!p.startsWith('/') && !DRIVE.test(p)) p = slash(r.cwd || r.writable?.[0] || '/') + '/' + p
  let root = '/'
  if (DRIVE.test(p)) {
    root = p.slice(0, 2) + '/'
    p = p.slice(2)
  }
  const parts: string[] = []
  for (const s of p.split('/')) {
    if (s === '' || s === '.') continue
    if (s === '..') parts.pop()
    else parts.push(s)
  }
  return root + parts.join('/')
}

// fold is p as paths are compared: with slashes, and case folded where
// the file system ignores case.
function fold(r: Rules, p: string): string {
  p = slash(p)
  return r.caseFold ? p.toLowerCase() : p
}

// under: p is root or inside it, by the file system's idea of case.
function under(r: Rules, p: string, root: string): boolean {
  p = fold(r, p)
  root = fold(r, root)
  return root !== '' && (p === root || p.startsWith(root.endsWith('/') ? root : root + '/'))
}

// isSecret: p is a credential store or inside one (covers).
function isSecret(r: Rules, p: string): boolean {
  return covers(r, r.secrets ?? [], p)
}

// covers: p is one of roots or inside one (credential stores, a mod's
// reads); a glob counts by the part before its first wildcard, and
// covers a root it could reach, unless it is the root of the file
// system or the home folder.
function covers(r: Rules, roots: readonly string[], p: string): boolean {
  const fixed = p.replace(/[*?[{].*$/, '').replace(/\/$/, '') || '/'
  const root = fixed === '/' || /^[A-Za-z]:\/?$/.test(fixed)
  return roots.some(s => under(r, fixed, s) || (fixed !== p && under(r, s, fixed) && !root && fold(r, fixed) !== fold(r, r.home ?? '')))
}

// isWorktreeRoot: removing p takes a worktree with it: the thread's own,
// tm's worktrees folder, a project's folder in it or a worktree in that.
function isWorktreeRoot(r: Rules, p: string): boolean {
  const own = r.role === 'thread' ? r.writable?.[0] : undefined
  if (own && under(r, own, p)) return true
  if (!r.worktrees) return false
  const wt = fold(r, r.worktrees)
  p = fold(r, p)
  if (under(r, wt, p)) return true
  if (!under(r, p, wt)) return false
  return p.slice(wt.length + 1).split('/').length <= 2
}

// safe keeps a path or branch to the characters a journal line needs.
function safe(s: string): string {
  return s.replace(/[^A-Za-z0-9._/~@+:-]/g, '?').slice(0, 100)
}

// PowerShell, as Claude's PowerShell tool runs it on Windows (a shell
// tool with syntax 'powershell'); internal/guard/powershell.go is its Go
// port. Its commands are split and read as Bash's are, with PowerShell's
// quoting, and judged by the same rules: git, gh, az and the HTTP
// clients as in Bash, plus Remove-Item and its aliases (delete-branch),
// Get-Content, Copy-Item and the like on a secret, the Env: drive and
// $env:AZURE_DEVOPS_EXT_PAT (credentials), Invoke-WebRequest and
// Invoke-RestMethod to Azure DevOps (as curl), and a command line handed
// to pwsh -Command, cmd /c or bash -c.
//
// Windows has no sandbox behind the guard, so what it can't read is put
// to the human (or auto mode's classifier) rather than let through: a
// command named by a variable or an expression (& $x, & (…)), a string
// evaluated (Invoke-Expression, Invoke-Command, Start-Process, Start-Job,
// Add-Type, Set-Alias, pwsh -EncodedCommand) and the .NET methods that
// run, read or delete ([Diagnostics.Process]::Start(…)).

// judgePowerShell checks each command of a PowerShell command line. A
// refusal wins over an ask.
function judgePowerShell(r: Rules, has: (id: string) => boolean, line: string): Denial | null {
  const m = PS_METHOD.exec(line)
  let ask: Denial | null = m ? askOf(`the .${m[1]}() method`) : null
  for (const words of psCommands(line)) {
    const d = judgePSCommand(r, has, words)
    if (d && !d.ask) return d
    ask ??= d
  }
  return ask
}

// askOf is the question for a command the guard can't read, what makes
// it so in why: the guard's own words or a name it knows, never the
// call's text.
function askOf(why: string): Denial {
  return { rule: 'unknown', ask: true, summary: why, message: `terminatr guard: it can't tell what this PowerShell command runs (${why}), so it asks first.` }
}

// A .NET method that runs, evaluates, reads or deletes.
const PS_METHOD = /(?:::|\.)\s*(Start|Invoke|InvokeScript|AddScript|AddCommand|Create|Run|Exec|ShellExecute|GetEnvironmentVariables?|ReadAllText|ReadAllBytes|ReadAllLines|ReadLines|OpenRead|OpenText|Delete|Move|Copy)\s*\(/i

// The cmdlets and aliases the guard knows, by lowercase name. PS_EVAL
// run what they are given as code, or a program with what they are
// given: asked. PS_LISTER list a drive's items, the Env: drive's being
// variables.
const setOf = (names: string) => new Set(names.split(' '))
const PS_REMOVE = setOf('remove-item rm ri del erase rd rmdir')
const PS_EVAL = setOf('invoke-expression iex invoke-command icm start-process saps start start-job sajb start-threadjob add-type set-alias sal new-alias nal')
const PS_READERS = setOf('get-content gc type copy-item copy cpi move-item move mi select-string sls format-hex fhx import-clixml')
const PS_PRINTERS = setOf('write-output write write-host write-information out-host out-string set-clipboard scb')
const PS_LISTER = setOf('get-childitem gci ls dir get-item gi get-itemproperty gp')
const PS_HTTP = setOf('invoke-webrequest iwr invoke-restmethod irm')

// psExpr is a command's first word without the casts it starts with
// ([string]$x): the next word when it is casts alone ([string] $x).
function psExpr(argv: string[]): string {
  const e = argv[0].replace(/^(?:\[[^\]]*\])+/, '')
  return e === '' && argv.length > 1 && argv[0] !== '' ? argv[1] : e
}

// judgePSCommand judges one simple command of a PowerShell line. One the
// guard judges by its arguments, given an expression as one ((…), $(…),
// @(…)), is asked unless refused.
function judgePSCommand(r: Rules, has: (id: string) => boolean, words: string[]): Denial | null {
  const [stripped, assigned] = psStrip(words)
  let argv = stripped
  if (argv.length === 0) return null
  const op = argv[0]
  if (op === '&' || op === '.') {
    if (argv.length === 1) return askOf(`${op} of an expression`)
    argv = argv.slice(1)
    if (argv[0].startsWith('$')) return askOf(`${op} of a variable`)
  } else if (psExpr(argv).startsWith('$')) {
    // An expression: printed unless assigned.
    const v = patVar(psExpr(argv))
    if (v && !assigned && has('credentials')) return deny(r, 'credentials', `output of ${safe(v)}`)
    return null
  }
  const cmd = psName(argv[0])
  const args = argv.slice(1)
  const d = judgePSArgs(r, has, cmd, argv, args)
  if (!d && args.some(psExprArg) && psJudged(r, cmd)) return askOf('an expression as an argument')
  return d
}

// psJudged: cmd is one the guard judges by its arguments.
function psJudged(r: Rules, cmd: string): boolean {
  return ['git', 'gh', 'az', 'pwsh', 'powershell', 'cmd'].includes(cmd) || SHELLS.has(cmd) || HTTP_CLIENTS.has(cmd) || PS_HTTP.has(cmd) ||
    PS_REMOVE.has(cmd) || READERS.has(cmd) || PS_READERS.has(cmd) || PS_LISTER.has(cmd) ||
    (r.mods ?? []).some(m => (m.commands ?? []).some(c => c.length > 0 && modWord(c[0], cmd, true)))
}

// judgePSArgs judges command cmd, of argv, by its arguments.
function judgePSArgs(r: Rules, has: (id: string) => boolean, cmd: string, argv: string[], args: string[]): Denial | null {
  const md = judgeMods(r, cmd, args, true)
  if (md) return md
  let d: Denial | null = null
  if (PS_EVAL.has(cmd)) return askOf(cmd)
  if (cmd === 'pwsh' || cmd === 'powershell') return judgePwsh(r, has, args)
  if (cmd === 'cmd') {
    const i = args.findIndex(a => ['/c', '/k', '/r'].includes(a.toLowerCase()))
    return i >= 0 ? judgeBash(r, has, args.slice(i + 1).join(' ')) : null
  }
  if (SHELLS.has(cmd)) {
    const i = args.findIndex(a => SHELL_C.test(a))
    return i >= 0 && i + 1 < args.length ? judgeBash(r, has, args[i + 1]) : null
  }
  if (cmd === 'git') d = judgeGit(r, has, args)
  else if (cmd === 'gh') d = judgeGh(r, has, args)
  else if (cmd === 'az') d = judgeAz(r, has, args)
  else if (HTTP_CLIENTS.has(cmd) || PS_HTTP.has(cmd)) {
    // In Windows PowerShell curl and wget are Invoke-WebRequest.
    if (HTTP_CLIENTS.has(cmd)) d = judgeHttp(r, has, cmd, args)
    if (!d && cmd !== 'http' && cmd !== 'https' && cmd !== 'xh') d = judgeWebRequest(r, has, args)
  } else if (PS_REMOVE.has(cmd) && has('delete-branch') && args.some(psRecursive)) {
    for (const p of psPaths(args)) {
      if (isWorktreeRoot(r, abs(r, p))) {
        d = deny(r, 'delete-branch', `Remove-Item -Recurse of ${safe(abs(r, p))}`)
        break
      }
    }
  }
  if (d) return d
  if (has('credentials')) {
    const paths = psPaths(args)
    return judgePSSecret(r, cmd, args) ??
      judgeSecret(r, cmd, paths, [argv[0], ...paths], READERS.has(cmd) || PS_READERS.has(cmd), PRINTERS.has(cmd) || PS_PRINTERS.has(cmd))
  }
  return null
}

// pwsh's options that take a value.
const PWSH_VALUED = ['executionpolicy', 'windowstyle', 'workingdirectory', 'outputformat', 'inputformat',
  'configurationname', 'configurationfile', 'settingsfile', 'version', 'psconsolefile', 'custompipename']

// judgePwsh: pwsh or powershell given a command line (-Command, or the
// first argument), judged as one; an encoded one is asked; a script
// (-File) is a script.
function judgePwsh(r: Rules, has: (id: string) => boolean, args: string[]): Denial | null {
  for (let i = 0; i < args.length; i++) {
    const a = args[i]
    if (!a.startsWith('-') && !a.startsWith('/')) return judgePowerShell(r, has, args.slice(i).join(' '))
    if (psParam(a, 'encodedcommand', 1) || a.toLowerCase() === '-ec') return askOf('an encoded command')
    if (psParam(a, 'command', 1)) return judgePowerShell(r, has, args.slice(i + 1).join(' '))
    if (psParam(a, 'file', 1)) return null
    if (PWSH_VALUED.some(n => psParam(a, n, 2)) || ['-ep', '-wd', '-of', '-if', '-v'].includes(a.toLowerCase())) i++
  }
  return null
}

// judgeWebRequest: Invoke-WebRequest or Invoke-RestMethod to Azure
// DevOps, judged as curl is: by what it authenticates with, then by
// method and route.
function judgeWebRequest(r: Rules, has: (id: string) => boolean, args: string[]): Denial | null {
  const url = args.find(a => AZURE_HOST.test(a))
  if (url === undefined) return null
  if (has('credentials') && args.some(a => psParam(a, 'headers', 1) || psParam(a, 'credential', 2) || psParam(a, 'token', 1) ||
    psParam(a, 'authentication', 2) || psParam(a, 'usedefaultcredentials', 2) || /authorization:|AZURE_DEVOPS_EXT_PAT/i.test(a))) {
    return deny(r, 'credentials', 'an HTTP call to Azure DevOps with credentials')
  }
  let method = ''
  for (let j = 0; j < args.length; j++) {
    const a = args[j]
    if (psParam(a, 'method', 2) || psParam(a, 'custommethod', 2)) method = psValue(a) ?? args[j + 1] ?? ''
  }
  return judgeRoute(r, has, method || 'GET', url)
}

// judgePSSecret: the Env: drive listed or read, all of it or a variable
// that looks secret.
function judgePSSecret(r: Rules, cmd: string, args: string[]): Denial | null {
  if (!PS_LISTER.has(cmd) && !PS_READERS.has(cmd) && !READERS.has(cmd)) return null
  for (const p of psPaths(args)) {
    const m = /^env:[\\/]?(.*)$/is.exec(p)
    if (!m) continue
    if (m[1] === '' || /[*?[]/.test(m[1])) return deny(r, 'credentials', `${safe(cmd)} printing the environment`)
    if (SECRET_NAME.test(m[1])) return deny(r, 'credentials', `${safe(cmd)} of ${safe(m[1])}`)
  }
  return null
}

// psRecursive: a's an option of Remove-Item's that makes it recursive
// (-Recurse, -r; rm.exe's -rf).
function psRecursive(a: string): boolean {
  if (psParam(a, 'recurse', 1)) {
    const v = psValue(a)
    return v === undefined || v.toLowerCase() !== '$false'
  }
  return /^-[a-zA-Z]*[rR]/.test(a) || a === '--recursive'
}

// psPaths are the paths among a cmdlet's arguments: those not options,
// the values of -Name:value options, each of a comma list.
function psPaths(args: string[]): string[] {
  const out: string[] = []
  for (let a of args) {
    if (a.startsWith('-')) {
      const v = psValue(a)
      if (v === undefined) continue
      a = v
    }
    for (const p of a.split(',')) if (p !== '') out.push(p)
  }
  return out
}

// psParam: a is the option -name, or the start of it at least min
// letters long, case aside, with or without a :value.
function psParam(a: string, name: string, min: number): boolean {
  if (!a.startsWith('-')) return false
  let n = a.slice(1).toLowerCase()
  const i = n.indexOf(':')
  if (i >= 0) n = n.slice(0, i)
  return n.length >= min && name.startsWith(n)
}

// psValue is the value of an option given as -name:value.
function psValue(a: string): string | undefined {
  const i = a.indexOf(':')
  return i >= 0 && a.startsWith('-') ? a.slice(i + 1) : undefined
}

// psName is a command's name as the guard knows it: its file name,
// lowercase, without .exe (C:\Program Files\Git\cmd\git.exe is git).
function psName(w: string): string {
  w = w.slice(Math.max(w.lastIndexOf('/'), w.lastIndexOf('\\')) + 1).toLowerCase()
  const ext = ['.exe', '.cmd', '.bat', '.com'].find(e => w.endsWith(e) && w.length > e.length)
  return ext ? w.slice(0, -ext.length) : w
}

// psStrip drops the assignments a command's output goes to ($x = git
// …), and says whether there were any.
function psStrip(words: string[]): [string[], boolean] {
  let assigned = false
  for (;;) {
    if (words.length === 0) return [words, assigned]
    const op = words.length > 1 && /^(?:\[[^\]]*\])*\$[^=\s]+$/.test(words[0]) ? /^(?:[-+*/%]|\?\?)?=([^]*)$/.exec(words[1]) : null
    const one = op ? null : /^(?:\[[^\]]*\])*\$[^=\s]*?(?:[-+*/%]|\?\?)?=([^]*)$/.exec(words[0])
    if (op) words = op[1] !== '' ? [op[1], ...words.slice(2)] : words.slice(2)
    else if (one) words = one[1] !== '' ? [one[1], ...words.slice(1)] : words.slice(1)
    else return [words, assigned]
    assigned = true
  }
}

// PowerShell takes typographic quotes as quotes.
const isSQuote = (c: string | undefined) => c === "'" || c === '\u2018' || c === '\u2019' || c === '\u201a' || c === '\u201b'
const isDQuote = (c: string | undefined) => c === '"' || c === '\u201c' || c === '\u201d' || c === '\u201e'

// psCommands splits a PowerShell command line into simple commands, each
// a list of words with quotes and escapes (`) removed: on ; | && ||
// newlines, ( ), $( ), @( ), @{ }, { } and a closing &, the inside of a
// $( ) in double quotes and here-strings read as commands too. & or .
// calling a command stays its first word ("&"); one calling a script
// block is dropped, the block's commands read in its place. Comments
// are skipped; redirections stay words (">"), their stream number
// dropped. An expression given as an argument ((…), $(…), @(…)) is a
// word of its command, marked (PS_EXPR_MARK), its commands read after it.
export function psCommands(line: string): string[][] {
  const out: string[][] = []
  let inner: string[][] = []
  let words: string[] = []
  let word = ''
  let inWord = false
  const endWord = () => {
    if (inWord) words.push(word)
    word = ''
    inWord = false
  }
  const endCommand = () => {
    endWord()
    if (words.length) out.push(words)
    // What its arguments evaluate, after it.
    out.push(...inner)
    words = []
    inner = []
  }
  const s = line
  // expandable is the inside of a double-quoted string from i, to its
  // end (closing(i) > 0 at the end: one past it, or the line's end), its
  // $( ) read as commands. In a plain string "" is a quote.
  const expandable = (i: number, closing: (k: number) => number, doubled: boolean): [string, number] => {
    let q = ''
    while (i < s.length) {
      if (doubled && isDQuote(s[i]) && i + 1 < s.length && isDQuote(s[i + 1])) {
        q += s[i]
        i += 2
        continue
      }
      const n = closing(i)
      if (n > 0) return [q, i + n]
      if (s[i] === '`' && i + 1 < s.length) {
        q += s[i + 1]
        i += 2
      } else if (s[i] === '$' && s[i + 1] === '(') {
        const end = psClose(s, i + 2)
        out.push(...psCommands(s.slice(i + 2, end)))
        q += s.slice(i, end + 1)
        i = end + 1
      } else q += s[i++]
    }
    return [q, i]
  }
  // What & or . may call: not nothing, a separator or a script block.
  const calls = (i: number) => {
    const j = psSkipBlank(s, i)
    return j < s.length && !';|\n\r)}&{'.includes(s[j])
  }
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c === '`') {
      if (i + 1 < s.length) {
        if (s[i + 1] === '\r' && s[i + 2] === '\n') i++
        else if (s[i + 1] !== '\n') {
          word += s[i + 1]
          inWord = true
        }
        i++
      }
    } else if (c === '<' && s[i + 1] === '#') {
      let j = i + 2
      while (j + 1 < s.length && !(s[j] === '#' && s[j + 1] === '>')) j++
      i = j + 1
    } else if (c === '#' && !inWord) {
      while (i + 1 < s.length && s[i + 1] !== '\n') i++
    } else if (c === '@' && !inWord && (isSQuote(s[i + 1]) || isDQuote(s[i + 1])) && psHereStart(s, i + 2) > 0) {
      // A here-string: @' or @" ending its line, to a line starting '@
      // or "@; from the newline that ends the opening line.
      const literal = isSQuote(s[i + 1])
      let j = psHereStart(s, i + 2) - 1
      const closing = (k: number) =>
        s[k] === '\n' && k + 2 < s.length && (literal ? isSQuote(s[k + 1]) : isDQuote(s[k + 1])) && s[k + 2] === '@' ? 3 : 0
      let body: string
      if (literal) {
        let k = j
        while (k < s.length && closing(k) === 0) k++
        body = s.slice(j, k)
        j = k + 3
      } else [body, j] = expandable(j, closing, false)
      word += body
      inWord = true
      i = j - 1
    } else if (isSQuote(c)) {
      let j = i + 1
      while (j < s.length) {
        if (isSQuote(s[j])) {
          if (isSQuote(s[j + 1])) {
            word += s[j]
            j += 2
            continue
          }
          break
        }
        word += s[j++]
      }
      inWord = true
      i = j
    } else if (isDQuote(c)) {
      const [q, j] = expandable(i + 1, k => (isDQuote(s[k]) ? 1 : 0), true)
      word += q
      inWord = true
      i = j - 1
    } else if (c === ' ' || c === '\t' || c === '\r') {
      endWord()
    } else if (c === '&') {
      if (s[i + 1] === '&') {
        endCommand()
        i++
      } else if (words.length === 0 && !inWord) {
        // The call operator, unless nothing follows (a background job's
        // &); calling a script block, the block's commands are read
        // instead.
        if (calls(i + 1)) words.push('&')
      } else endCommand() // a background job
    } else if (c === '.' && !inWord && words.length === 0 && (s[i + 1] === ' ' || s[i + 1] === '\t')) {
      // Dot-sourcing, as & is read.
      if (calls(i + 1)) words.push('.')
    } else if (c === '$' && s[i + 1] === '{') {
      // A braced variable, ${env:NAME}: one word to its }, which a
      // backtick escapes.
      let j = i + 2
      while (j < s.length && s[j] !== '}') j += s[j] === '`' ? 2 : 1
      word += s.slice(i, j + 1)
      inWord = true
      i = j
    } else if ((c === '(' || ((c === '$' || c === '@') && s[i + 1] === '(' && !(inWord && words.length === 0))) && psArgument(words, word, inWord)) {
      // An expression as an argument: a word of its own (marked), its
      // commands read too.
      endWord()
      const start = c === '(' ? i + 1 : i + 2
      const end = psClose(s, start)
      inner.push(...psCommands(s.slice(start, end)))
      word += PS_EXPR_MARK + s.slice(i, end + 1)
      inWord = true
      endWord()
      i = end
    } else if (';|\n(){}'.includes(c)) {
      endCommand()
    } else if ((c === '$' || c === '@') && (s[i + 1] === '(' || (c === '@' && s[i + 1] === '{'))) {
      endCommand()
      i++
    } else if (c === '>') {
      // 2>, *>: the stream number is no word.
      if (inWord && (word === '*' || /^[0-9]*$/.test(word))) {
        word = ''
        inWord = false
      }
      endWord()
      words.push('>')
      if (s[i + 1] === '>') i++
      if (i + 2 < s.length && s[i + 1] === '&' && s[i + 2] >= '0' && s[i + 2] <= '9') i += 2
    } else if (c === '<') {
      endWord()
      words.push('<')
    } else if (!inWord && (c === '\u2013' || c === '\u2014' || c === '\u2015')) {
      // PowerShell takes a dash for an option's hyphen.
      word += '-'
      inWord = true
    } else {
      word += c
      inWord = true
    }
  }
  endCommand()
  return out
}

// PS_EXPR_MARK starts the word psCommands makes of an expression given
// as an argument ((…), $(…), @(…)): no text a command line holds.
const PS_EXPR_MARK = '\u0000'

// psExprArg: a is an expression given as an argument.
const psExprArg = (a: string) => a.startsWith(PS_EXPR_MARK)

// PS_KEYWORDS take a condition or a value in parentheses, not arguments.
const PS_KEYWORDS = setOf('if elseif else while for foreach switch until do function filter param return throw trap catch finally exit')

// psArgument: a ( after words (and the word being read, when inWord) is
// in argument position: there is a command it is an argument of, not an
// expression, a keyword or a bare & or . before it. (A $( or @( glued to
// the command's name may be part of the name: psCommands reads it as
// before, a command of its own.)
function psArgument(words: string[], word: string, inWord: boolean): boolean {
  let [argv] = psStrip(inWord ? [...words, word] : words)
  if (argv.length > 0 && (argv[0] === '&' || argv[0] === '.')) argv = argv.slice(1)
  if (argv.length === 0) return false
  const head = argv[0]
  return !head.startsWith('$') && !head.startsWith('[') && !head.startsWith('(') && !psExprArg(head) && !PS_KEYWORDS.has(head.toLowerCase())
}

// psHereStart is where a here-string's body starts when its opening
// quote ends a line (from i, after @' or @"), or 0.
function psHereStart(s: string, i: number): number {
  while (i < s.length && (s[i] === ' ' || s[i] === '\t' || s[i] === '\r')) i++
  return s[i] === '\n' ? i + 1 : 0
}

// psSkipBlank is the first index from i that isn't a space or tab.
function psSkipBlank(s: string, i: number): number {
  while (i < s.length && (s[i] === ' ' || s[i] === '\t')) i++
  return i
}

// psClose is the index of the ) that closes a ( opened before i,
// skipping quoted strings, or s.length.
function psClose(s: string, i: number): number {
  let depth = 1
  for (; i < s.length; i++) {
    const c = s[i]
    if (c === '`') i++
    else if (c === '(') depth++
    else if (c === ')') {
      if (--depth === 0) return i
    } else if (isSQuote(c) || isDQuote(c)) {
      for (i++; i < s.length && !((isSQuote(c) && isSQuote(s[i])) || (isDQuote(c) && isDQuote(s[i]))); i++) {
        if (s[i] === '`' && isDQuote(c)) i++
        // A $( ) in the string: its quotes are its own.
        else if (s[i] === '$' && isDQuote(c) && s[i + 1] === '(') i = psClose(s, i + 2)
      }
    }
  }
  return s.length
}
