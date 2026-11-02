package program

// The texts tm writes into a new program folder. PROGRAM.md's body,
// CONTEXT.md and MEMORY.md are the director's and the user's to change
// afterwards (PROGRAM.md's body only on the user's word).

const instructionsTemplate = `# Instructions

Standing instructions for this program: what its projects share, the
conventions they follow, what never to do. Every member project's
coordinator is pointed at this file. The director and the human edit it;
membership and authority live in ~/.terminatr/config.toml, not here.
`

const contextTemplate = `# Context

Living context for this program: the current plan across its projects,
cross-project decisions and what is waiting on what. The director keeps
it current.

## Needs you

Open questions to the user, one line each, until they answer: a cleared
conversation loses none.
`

const memoryTemplate = `# Memory

tm writes the list below from the notes in memory/ after every change:
one line per note, a link to it, what it holds and (touches: a, b).
Notes marked standing: true reach every member project's threads.
`
