// The mod socket (TERMINATR_MOD_SOCKET): a Unix socket that speaks HTTP
// to the server, the one way the hooks talk to it. What a request over
// it looks like and what a failed one logs are here; the calls stay in
// each hooks file, since $ is followed only into functions of the file
// that uses it (claude plugin validate).

// socketURL is the server's URL for path ("/v1/state").
export function socketURL(path: string): string {
  return `http://terminatr${path}`
}

// postInit is a POST over socket: body as JSON, or no body at all.
export function postInit(socket: string, body?: string) {
  return body === undefined
    ? { method: 'POST' as const, socketPath: socket }
    : { method: 'POST' as const, socketPath: socket, headers: { 'content-type': 'application/json' }, body }
}

// refused is the debug line for an answer that isn't 2xx.
export function refused(what: string, status: number, text: string): string {
  return `terminatr: ${what}: ${status} ${text}`
}

// failed is the debug line for a request that didn't get an answer.
export function failed(what: string, err: unknown): string {
  return `terminatr: ${what}: ${String(err)}`
}

// Deadlines on the socket: a server that is alive but stuck must not hold
// a hook, a tool call or the report queue up. A tool call may take a
// while (the server runs tm behind it); an ack, state, log or usage post
// answers at once or not at all.
export const TOOL_MS = 30_000
export const POST_MS = 4_000
export const FETCH_MS = 2_000

// within is work, or null when wait (a $.clock.sleep) is over first.
export function within<T>(work: Promise<T>, wait: Promise<unknown>): Promise<T | null> {
  return Promise.race([work, wait.then(() => null)])
}

// timedOut is the debug line for a request that got no answer in time.
export function timedOut(what: string): string {
  return `terminatr: ${what}: no answer in time`
}
