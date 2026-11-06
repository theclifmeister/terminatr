# Week in review: {title}

A page the user can send to someone: the numbers come from tm, the
headlines from whoever understood the week. Coordinator: write it yourself,
or start a thread on it and give it the headlines you know.

The numbers: `tm week`, or `tm week --json`; `--weeks N` for a longer
stretch. Copy them as they stand. Each repo keeps its own release range,
with no total across repos. What the numbers leave out stays out of the
page: no releases means no versions line, no designs means no chapters. If
nothing finished, say so and give the last finish date; don't fill the page.

The headlines: what changed for the people who use the work, in plain words
("The Mac app ships"), never a list of task titles.

The page has no task or thread numbers. It isn't code and needs no pull
request: write it outside the repo, then `tm report --attach FILE` so it
reaches the Library (a coordinator writing it itself: `tm library add FILE`).

## Steps

- Read the numbers with tm week --json
- Read what happened in the stretch: the reports, the journal, tm search
- Write the page: headlines first, then the numbers as tm gives them
- Check the page names no task or thread number
- Attach the page to the report, so it reaches the Library
