/**
 * Recorded `/v1/messages` bodies carrying prompts at the length real traffic
 * arrives in. The fixtures in `requests.ts` cover wire shapes; these exist to
 * measure what compression saves on prose, so the text is deliberately uneven —
 * different registers, different code-to-prose ratios, different turn counts.
 */
import type { RequestFixture } from './fixture.js';

const beginnerRamble: RequestFixture = {
  name: 'beginner rambling about a react state bug',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "ok so this is probably a dumb question but i've been stuck on this for like two days now and i'm losing my mind. i have a form component and when i type into the input the value doesn't update, or actually it does update but only after i click somewhere else on the page which is weird right? i'm using useState and i set it up the way the tutorial did. the tutorial was from 2021 so maybe something changed idk. also the console doesn't show any errors at all which makes it harder because i have nothing to google.\n\nwhat i tried so far: i tried adding a key to the input, that did nothing. i tried moving the useState into the parent and passing it down as props, same behavior. i tried console.logging inside the onChange and it does fire, every keystroke, and the value in the log is correct, so the handler is fine i think? but then the input still shows the old value. someone on discord said something about controlled vs uncontrolled components but when i read the react docs about that i honestly did not follow what they were saying, it talked about defaultValue and i'm not using defaultValue anywhere.\n\none more thing that might matter or might not, the form is inside a modal that gets rendered with a portal. i don't know if that changes anything but i figured i'd mention it since the modal was the last thing i added before this broke. actually now that i type that out, it might be the modal? but i don't know why it would be.\n\nany idea what i'm doing wrong here",
          },
        ],
      },
    ],
  },
};

const formalBugReport: RequestFixture = {
  name: 'formal bug report with stack trace and package manifest',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 8192,
    system:
      'You are helping a backend engineer diagnose production incidents. Ask for missing evidence before guessing. When you propose a fix, say what would have to be true for it to work.',
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'Our ingest worker started crashing in production about four hours after we shipped release 2.14.0. It runs fine locally and it ran fine in staging for a full day under synthetic load, so whatever this is, it depends on something about the production data or the production environment that we are not reproducing.\n\nThe process exits with a non-zero code roughly every ninety seconds, the supervisor restarts it, it chews through part of the queue and then dies again. We have lost about eleven thousand messages so far because the retry topic is also filling up. Here is the trace we get on every crash, it is identical each time apart from the offset in the last frame:\n\n```\nTypeError: Cannot read properties of undefined (reading \'toISOString\')\n    at normalizeEvent (/srv/ingest/dist/pipeline/normalize.js:142:38)\n    at /srv/ingest/dist/pipeline/batch.js:87:22\n    at Array.map (<anonymous>)\n    at processBatch (/srv/ingest/dist/pipeline/batch.js:86:31)\n    at async Worker.handle (/srv/ingest/dist/worker.js:203:5)\n    at async Runner.loop (/srv/ingest/dist/runner.js:64:9)\n```\n\nThe line it dies on reads `const stamp = event.occurredAt.toISOString()` so clearly some events are arriving without an occurredAt, which we thought was impossible because the producer sets it. We have not changed the producer in six weeks.\n\nDependencies from the release, in case one of these is the actual culprit:\n\n```json\n{\n  "dependencies": {\n    "@app/schema": "4.2.1",\n    "kafkajs": "2.2.4",\n    "pino": "9.5.0",\n    "zod": "3.23.8"\n  }\n}\n```\n\nThe only thing that moved in 2.14.0 was a bump of @app/schema from 4.1.9 to 4.2.1 and a change to how we batch, we went from batches of 50 to batches of 500. I suspect the schema bump but I cannot see how a schema package would strip a field. What would you look at first, and is there anything I should capture off the box before we roll back and lose the evidence?',
          },
        ],
      },
    ],
  },
};

const terseExpertSql: RequestFixture = {
  name: 'terse expert asking about a postgres plan',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 2048,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "Why is this seq scanning. Index exists on (tenant_id, created_at desc), stats were analyzed an hour ago, table is ~40M rows.\n\n```sql\nselect id, payload\nfrom events\nwhere tenant_id = $1\n  and created_at >= now() - interval '7 days'\norder by created_at desc\nlimit 100;\n```\n\n```\nLimit  (cost=0.00..184320.55 rows=100 width=612) (actual time=8123.442..8123.501 rows=100 loops=1)\n  ->  Seq Scan on events  (cost=0.00..2211846.60 rows=1200 width=612)\n        Filter: ((tenant_id = 17) AND (created_at >= (now() - '7 days'::interval)))\n        Rows Removed by Filter: 39982104\nPlanning Time: 0.212 ms\nExecution Time: 8123.559 ms\n```\n\ntenant_id is bigint, param is passed as a JS number through pg. Cardinality on tenant 17 is about 900k rows. Row estimate of 1200 is nonsense.",
          },
        ],
      },
    ],
  },
};

const supportAgentSystem: RequestFixture = {
  name: 'long support-agent system prompt with a casual question',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    system: [
      {
        type: 'text',
        text: 'You are the front-line support assistant for Trellis, a subscription tool that teams use to schedule shift work. You answer questions from account owners, shift managers and hourly staff, and the three groups need different answers to the same question, so find out who you are talking to before you explain anything about permissions.\n\nWhat you can do: explain how a feature works, walk someone through a settings change step by step, read back what their plan includes, and tell them whether something is possible on their tier. What you cannot do: change anything on the account, issue a refund, extend a trial, or see payment details. If somebody asks for one of those, say plainly that you cannot do it and hand them to a human with a one-line summary of what they want.\n\nBilling questions are the ones people get angry about, so be careful there. Prorating on mid-cycle upgrades confuses almost everyone: the charge they see on the invoice is the remainder of the current period at the new rate, not a full month, and the next invoice is the first one at the full new price. Say that in plain words rather than quoting the billing docs at them. If somebody thinks they were charged twice, do not argue about it, take the invoice numbers and pass it on.\n\nTone: short sentences, no exclamation marks, no apologising more than once in a conversation. People who write in are usually mid-shift and want the answer, not warmth. If you do not know, say you do not know and say who does.',
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "hey so we upgraded to the team plan halfway through last month and the invoice i just got is way smaller than i expected, like 40 bucks instead of the 99 on the pricing page. did it not go through properly? we do have all the seats working so i think it worked but i don't want to get a surprise bill later",
          },
        ],
      },
    ],
  },
};

const multiTurnDebugging: RequestFixture = {
  name: 'six-turn debugging conversation that goes sideways',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 8192,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'Our nightly job that syncs users from Okta into our own database has started timing out. It used to take about four minutes and now it either takes forty minutes or it never finishes and gets killed by the scheduler at the one hour mark. Nothing in our code changed this week as far as I can tell. Where would you start?',
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'A job that went from four minutes to forty without a code change usually means one of three things moved underneath it: the amount of data, the shape of a query, or a rate limit on the API you are pulling from.\n\nStart by finding out which phase is slow. If the job logs timing per phase, pull last week and this week side by side. If it does not, the cheapest instrumentation is a timestamp before and after the fetch loop and before and after the write loop.\n\nA few questions that would narrow it quickly:\n\n- How many users does the sync pull now versus a month ago?\n- Does it page through the Okta API, and if so what page size?\n- Are the writes done one row at a time or in batches?',
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "ok i added timing. the fetch loop is 90 seconds, basically unchanged. the write loop is the whole rest of it. we have 31k users now, we had about 28k a month ago so that grew a bit but not 10x.\n\nwrites are one at a time, it's a for loop doing an upsert per user. that's been like that since forever though",
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'One upsert per user at 31k users means 31k round trips, and if each one now costs a little more than it used to, the total blows up while nothing looks obviously broken.\n\nTwo things to check on the write path:\n\nFirst, what indexes are on the users table right now. Every index has to be maintained on every upsert, so an index somebody added recently for a dashboard query would tax all 31k writes.\n\nSecond, whether the upserts run inside one transaction or each in its own. Thirty-one thousand separate transactions each pay a commit.',
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'someone added two indexes eight days ago. one of them is a gin index on a jsonb column called profile_attributes. that timing lines up almost exactly with when this started.\n\nso is the fix just to drop the gin index? the dashboard person will not be happy but 40 minutes is not sustainable',
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'Dropping it would fix the sync and break whatever query it was added for, so it is worth spending ten minutes before you do that.\n\nA GIN index on jsonb is expensive to update, and by default Postgres buffers those updates in a pending list that gets flushed periodically, which makes the cost arrive in lumps rather than evenly. Check whether `fastupdate` is on for that index and what `gin_pending_list_limit` is set to.\n\nThe other angle is the write loop itself. Batching the upserts into groups of 500 with a single multi-row insert and an `on conflict` clause would cut round trips by a factor of 500, and the index maintenance would still happen but you would stop paying per-statement overhead 31 thousand times. That fix helps whether or not the index stays.',
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "fastupdate is on, default limit. ok i'll try batching first since that doesn't require me to go argue with anyone. one thing i'm unsure about: if i batch the upserts into 500-row statements, what happens to my error handling? right now if one user fails to upsert i log it and continue with the rest. with a batch the whole statement fails and i lose the other 499, and honestly some of these records are dodgy, we get nulls in fields that are supposed to be required. how do people usually handle that",
          },
        ],
      },
    ],
  },
};

const toolResultJsonPayload: RequestFixture = {
  name: 'tool conversation with a large json tool_result',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    tools: [
      {
        name: 'fetch_orders',
        description:
          'Fetch recent orders for a customer account, most recent first. Returns the raw response from the orders service.',
        input_schema: {
          type: 'object',
          properties: {
            account_id: { type: 'string' },
            limit: { type: 'integer' },
          },
          required: ['account_id'],
        },
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "customer on the phone says she was charged three times for the same order this morning and she wants to know which ones are real before she calls her bank. her account id is acct_9f21c. can you pull her recent orders and tell me what actually happened, she's been on hold for a while so quickly if you can",
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'Pulling her recent orders now.',
          },
          {
            type: 'tool_use',
            id: 'toolu_01Qx7',
            name: 'fetch_orders',
            input: { account_id: 'acct_9f21c', limit: 10 },
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: 'toolu_01Qx7',
            content:
              '{\n  "account_id": "acct_9f21c",\n  "orders": [\n    {\n      "id": "ord_8812af",\n      "created_at": "2025-03-11T09:14:02Z",\n      "status": "captured",\n      "amount_cents": 8400,\n      "currency": "usd",\n      "payment_intent": "pi_3Nx8812af",\n      "idempotency_key": "cart-7731-a",\n      "items": [{ "sku": "TRL-STD-01", "qty": 2, "unit_cents": 4200 }]\n    },\n    {\n      "id": "ord_8812b0",\n      "created_at": "2025-03-11T09:14:19Z",\n      "status": "failed",\n      "failure_code": "card_declined",\n      "amount_cents": 8400,\n      "currency": "usd",\n      "payment_intent": "pi_3Nx8812b0",\n      "idempotency_key": "cart-7731-b",\n      "items": [{ "sku": "TRL-STD-01", "qty": 2, "unit_cents": 4200 }]\n    },\n    {\n      "id": "ord_8812b4",\n      "created_at": "2025-03-11T09:15:47Z",\n      "status": "authorized",\n      "amount_cents": 8400,\n      "currency": "usd",\n      "payment_intent": "pi_3Nx8812b4",\n      "idempotency_key": "cart-7731-c",\n      "captured_at": null,\n      "items": [{ "sku": "TRL-STD-01", "qty": 2, "unit_cents": 4200 }]\n    }\n  ],\n  "has_more": false\n}',
          },
          {
            type: 'text',
            text: 'she says her banking app shows three pending charges of $84 each, that matches the count but i thought only one of these actually took her money',
          },
        ],
      },
    ],
  },
};

const toolResultLogWall: RequestFixture = {
  name: 'tool conversation with a wall of log output',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    tools: [
      {
        name: 'run_command',
        description:
          'Run a shell command in the deployment environment and return its output.',
        input_schema: {
          type: 'object',
          properties: { command: { type: 'string' } },
          required: ['command'],
        },
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'the api pods keep restarting after the deploy this morning. readiness probe fails but the app looks like it boots ok when i exec in and curl it myself. can you look at the logs from the last crashed pod and tell me what you make of it',
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'tool_use',
            id: 'toolu_02Lm3',
            name: 'run_command',
            input: { command: 'kubectl logs api-7d9c5f8b6-x2klm --previous --tail=40' },
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: 'toolu_02Lm3',
            content: [
              {
                type: 'text',
                text: '2025-03-11T06:02:11.004Z INFO  boot: starting api v2.14.0 pid=1 node=v20.11.1\n2025-03-11T06:02:11.188Z INFO  config: loaded 47 keys from env, 12 from /etc/api/config.yaml\n2025-03-11T06:02:11.402Z INFO  db: pool created min=2 max=20 host=pg-primary.internal\n2025-03-11T06:02:11.719Z INFO  db: connected in 317ms\n2025-03-11T06:02:11.720Z INFO  cache: connecting redis://redis-0.internal:6379\n2025-03-11T06:02:12.031Z WARN  cache: connect attempt 1 failed ECONNREFUSED, retrying in 500ms\n2025-03-11T06:02:12.544Z WARN  cache: connect attempt 2 failed ECONNREFUSED, retrying in 1000ms\n2025-03-11T06:02:13.560Z WARN  cache: connect attempt 3 failed ECONNREFUSED, retrying in 2000ms\n2025-03-11T06:02:15.577Z INFO  cache: connected in 3857ms\n2025-03-11T06:02:15.580Z INFO  http: listening on 0.0.0.0:8080\n2025-03-11T06:02:15.581Z INFO  boot: ready in 4577ms\n2025-03-11T06:02:16.114Z INFO  http: GET /healthz 200 1ms\n2025-03-11T06:02:26.109Z INFO  http: GET /healthz 200 0ms\n2025-03-11T06:02:31.882Z INFO  http: GET /readyz 200 12ms\n2025-03-11T06:02:36.110Z INFO  http: GET /healthz 200 1ms\n2025-03-11T06:02:41.903Z ERROR http: GET /readyz 503 5021ms deps=db:ok,cache:timeout,search:ok\n2025-03-11T06:02:46.112Z INFO  http: GET /healthz 200 0ms\n2025-03-11T06:02:51.918Z ERROR http: GET /readyz 503 5008ms deps=db:ok,cache:timeout,search:ok\n2025-03-11T06:02:56.111Z INFO  http: GET /healthz 200 1ms\n2025-03-11T06:03:01.925Z ERROR http: GET /readyz 503 5014ms deps=db:ok,cache:timeout,search:ok\n2025-03-11T06:03:02.004Z WARN  cache: command timed out after 5000ms cmd=PING\n2025-03-11T06:03:02.005Z WARN  cache: pool exhausted, 0 idle 50 pending\n2025-03-11T06:03:06.110Z INFO  http: GET /healthz 200 0ms\n2025-03-11T06:03:11.930Z ERROR http: GET /readyz 503 5011ms deps=db:ok,cache:timeout,search:ok\n2025-03-11T06:03:12.441Z INFO  signal: received SIGTERM, draining\n2025-03-11T06:03:12.442Z INFO  http: closing listener, 50 in-flight\n2025-03-11T06:03:42.443Z ERROR shutdown: drain timed out after 30000ms, 50 requests abandoned\n2025-03-11T06:03:42.444Z INFO  boot: exit code 1',
              },
            ],
          },
          {
            type: 'text',
            text: 'so it connects to redis fine at boot and then five minutes later every readyz check says cache timeout. the redis pod itself has not restarted, i checked. we did change the redis client library in this release, went from ioredis to node-redis, but the config looks equivalent to me. is the pool exhausted line the thing to chase or is that a symptom',
          },
        ],
      },
    ],
  },
};

const codeReviewPaste: RequestFixture = {
  name: 'code review request that is mostly a pasted diff',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "quick review before i put this up. mainly worried about the retry logic, i wrote it late\n\n```ts\nexport type RetryOptions = {\n  attempts: number;\n  baseDelayMs: number;\n  maxDelayMs: number;\n  isRetryable: (error: unknown) => boolean;\n};\n\nconst DEFAULTS: RetryOptions = {\n  attempts: 3,\n  baseDelayMs: 200,\n  maxDelayMs: 5_000,\n  isRetryable: () => true,\n};\n\nfunction delayFor(attempt: number, options: RetryOptions): number {\n  const exponential = options.baseDelayMs * 2 ** attempt;\n  const capped = Math.min(exponential, options.maxDelayMs);\n  return capped / 2 + Math.random() * (capped / 2);\n}\n\nexport async function withRetry<T>(\n  operation: () => Promise<T>,\n  overrides: Partial<RetryOptions> = {},\n): Promise<T> {\n  const options = { ...DEFAULTS, ...overrides };\n  let lastError: unknown;\n  for (let attempt = 0; attempt < options.attempts; attempt += 1) {\n    try {\n      return await operation();\n    } catch (error) {\n      lastError = error;\n      if (!options.isRetryable(error)) throw error;\n      await sleep(delayFor(attempt, options));\n    }\n  }\n  throw lastError;\n}\n```\n\nthings i already know are wrong: no abort signal, and sleeping after the last attempt is wasteful. what i actually want to know is whether the jitter is doing what i think it's doing and whether throwing lastError loses the stack. also is `attempts: 3` three total or three retries plus the first call, i keep flip flopping on which one callers expect and i've seen both conventions in libraries we use.",
          },
        ],
      },
    ],
  },
};

const casualOneLiner: RequestFixture = {
  name: 'casual one-liner with a pasted link',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content:
          "is this still the right way to do it or is it outdated https://blog.example.com/posts/2019/managing-secrets-in-ci ? my teammate sent it to me but its from 2019 adn we're on github actions now not travis",
      },
    ],
  },
};

const marketingProse: RequestFixture = {
  name: 'dense prose with no code at all',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'I need help rewriting the About page for our company and I think the current version is the problem, not the product. Let me give you the background first and then paste what we have.\n\nWe sell scheduling software to independent restaurants. Most of our customers are owner-operators with between one and four locations, and the person who actually buys the thing is usually the owner, who is also often the person doing the schedule at eleven at night on a laptop in the back office. They are not buying software because they love software. They are buying it because they lost a Sunday to a spreadsheet and a chain of text messages about who is covering the brunch shift.\n\nThe page we have now was written by an agency two years ago when we were trying to sell to restaurant groups with fifty locations, and that market never worked out for us. So the copy talks about enterprise deployment and multi-region rollouts and it opens with a sentence about how we were founded by two brothers who wanted to change the way the world works. Nobody who reads that page is a fifty-location group anymore. They are one guy with a bar and a wine list.\n\nWhat I want is a page that a busy owner would read the first two sentences of and think, these people have met a restaurant. It should be short. It should not have a mission statement. It can mention the founders but only if it says something a customer would care about, and honestly I am not sure the founder story earns its space at all.\n\nThe constraint from our side is that the legal team will not let us say anything about time saved unless we can point to a study, and we do not have one, so no percentages and no claims about hours back per week. We can say what the software does and we can describe the situation the customer is in.\n\nCan you draft two versions, one that keeps a short founder paragraph and one that drops it entirely, so I can put both in front of the team and let them argue about it.',
          },
        ],
      },
    ],
  },
};

const migrationPlanningWithCache: RequestFixture = {
  name: 'migration planning with a cached repo-context prefix',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 16384,
    system: [
      {
        type: 'text',
        text: 'You are a staff engineer reviewing a migration plan. Be concrete about ordering and rollback. Point out the step that cannot be undone.',
      },
      {
        type: 'text',
        text: 'Repository context, stable across this conversation.\n\nThe service is a Rails 6.1 monolith called `billing-core` that owns invoices, subscriptions and the ledger. It runs on Heroku with a single Postgres 13 primary and one follower used for reporting. There are 34 background job classes on Sidekiq, of which 6 touch the ledger. The ledger table is append-only and currently holds 240 million rows across four years, partitioned by nothing at all.\n\nDeploys go out roughly eight times a day through a GitHub Actions pipeline that runs the full test suite, about 22 minutes, then a `heroku releases:phase` rollout. Migrations run as a release phase command, so a slow migration blocks the deploy and eventually times out at 30 minutes.\n\nThe team is four engineers, two of whom have never run a production migration on this codebase.',
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'We want to partition the ledger table by month. Here is the plan I sketched out, tell me where it falls apart.\n\nStep one, create the partitioned table `ledger_entries_partitioned` with the same columns and monthly range partitions covering the last four years plus twelve months ahead. Step two, add a trigger on the old table that copies every insert into the new one so they stay in sync. Step three, backfill the four years of history in chunks of 50k rows with a rake task we run manually from a one-off dyno. Step four, once the backfill is caught up, take a two minute write pause, rename both tables, drop the trigger. Step five, delete the old table a week later once we are confident.\n\nMy worries in order: the backfill is going to take days at 50k a chunk and I do not know how to make that safe to interrupt and resume. The rename in step four feels like the moment everything breaks and I cannot picture how to roll it back if the app comes up pointing at a half-populated table. And I have no idea what the six ledger-touching Sidekiq jobs do during the write pause, some of them run every minute.\n\nAlso our reporting follower has queries against `ledger_entries` that nobody on the team wrote and I am not sure who owns.',
          },
        ],
      },
    ],
  },
};

const nonNativeDevOps: RequestFixture = {
  name: 'long meandering devops question with a compose file',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 8192,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'Hello, I have problem with my docker setup since two weeks and my colleague also cannot understand it, so maybe you can help me. Sorry if my explanation is not so clear.\n\nThe situation is like this. We have small application, is a Python API with a worker and a Postgres, and everything works perfect on my machine and on my colleague machine also. But when we deploy to the server of the customer, the worker container it starts and after maybe 30 seconds it stops, no error message, exit code 0. Not 1, is 0, which I find very strange because for me exit 0 means it finished normally? But it should never finish, it should run forever and wait for jobs.\n\nOn the server the docker version is older, is 20.10 something, and on our machines we have 24. I don\'t know if this is important but I mention it.\n\nThis is our compose file, I removed the passwords:\n\n```yaml\nservices:\n  api:\n    build: .\n    command: uvicorn app.main:app --host 0.0.0.0 --port 8000\n    ports:\n      - "8000:8000"\n    depends_on:\n      - db\n    environment:\n      DATABASE_URL: postgresql://app:REDACTED@db:5432/app\n      REDIS_URL: redis://cache:6379/0\n  worker:\n    build: .\n    command: celery -A app.worker worker --loglevel=info\n    depends_on:\n      - db\n      - cache\n    environment:\n      DATABASE_URL: postgresql://app:REDACTED@db:5432/app\n      REDIS_URL: redis://cache:6379/0\n  db:\n    image: postgres:15\n    volumes:\n      - pgdata:/var/lib/postgresql/data\n    environment:\n      POSTGRES_USER: app\n      POSTGRES_PASSWORD: REDACTED\n      POSTGRES_DB: app\n  cache:\n    image: redis:7\nvolumes:\n  pgdata:\n```\n\nWhat we tried already: we added restart: always to the worker, then it restarts in loop forever every 30 seconds which is not better, just more noise in the logs. We tried to run the same command with docker run manually on the server and then it works and stays running, which makes no sense to me because is the same image and same command.\n\nAlso when I do docker compose logs worker I see the celery banner and it prints ready, and then nothing, and then the container is gone. There is no traceback anywhere. My colleague thinks is OOM but the server has 16GB and nothing else runs there, and I think if OOM the exit code would be 137 not 0.\n\nDo you have idea what to look for? I am not so experienced with docker so maybe is something obvious that I don\'t see.',
          },
        ],
      },
    ],
  },
};

const apiDesignDiscussion: RequestFixture = {
  name: 'four-turn api design discussion with mixed prose and snippets',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 8192,
    system:
      'You are reviewing API designs. Prefer the option that is easier to change later over the one that is cleaner today, and say when the two point the same way.',
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'We are adding webhooks to our public API and I want to get the payload shape right before anyone depends on it, because once customers write handlers against it we are stuck with it more or less forever.\n\nThe argument on the team is about whether the payload should carry the full object or just an id and a type. The people who want the full object say it saves a round trip and most consumers just want the fields. The people who want the thin payload say the full object goes stale between the write and the delivery, and it leaks fields that the receiving integration might not be allowed to see.',
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'Both camps are right about different failure modes, and the tiebreaker is usually which mistake you can walk back.\n\nA thin payload can grow into a fat one later without breaking anyone: adding fields is additive. A fat payload cannot shrink, because someone is reading every field you ship, including the ones you regret.\n\nThe staleness point is real and it is not a matter of taste. Delivery is retried, sometimes hours later, and a payload delivered at 14:00 describing state as of 13:12 will be treated as current by a naive handler. If you ship the object, ship it with the version or timestamp it describes so a handler can tell.\n\nThe permissions point is the one I would weigh heaviest. A webhook endpoint is a different trust boundary from your API, and reusing your internal serializer there is how fields leak.',
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: 'ok that lands. so something like\n\n```json\n{\n  "id": "evt_01HW",\n  "type": "invoice.finalized",\n  "created_at": "2025-03-11T09:14:02Z",\n  "data": { "id": "inv_88f2", "object": "invoice" }\n}\n```\n\nand then they call GET /v1/invoices/inv_88f2 to get the rest. two follow ups. first, do we version the event type or the envelope, i\'ve seen both and i can\'t tell which one bites less. second, what do we do about deletes, because by the time they call back the object is gone',
          },
        ],
      },
      {
        role: 'assistant',
        content: [
          {
            type: 'text',
            text: 'Version the envelope, not the type. If versioning lives in the type name you end up with `invoice.finalized.v2` and every consumer has to subscribe twice during a migration, and your event catalog doubles. An envelope version lets a consumer pin one number and lets you serve two shapes off one subscription.\n\nDeletes are the case where the thin payload genuinely fails, so treat them as the exception rather than reworking the rule. For a delete event, inline the fields a handler needs to act on — the id, whatever they key their local record by, and the deletion timestamp. A handler that receives `customer.deleted` needs enough to find its own row, and nothing else.',
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'text',
            text: "makes sense. last thing and then i'll go write the rfc: retries. current thinking is exponential backoff for 24 hours then we drop it and mark the endpoint unhealthy, and after three consecutive unhealthy periods we disable the subscription and email whoever owns the integration. is 24 hours the normal number? and do people actually disable subscriptions in practice or does that just generate angry support tickets when someone's staging endpoint was down over a weekend",
          },
        ],
      },
    ],
  },
};

export const PROMPT_FIXTURES: readonly RequestFixture[] = [
  beginnerRamble,
  formalBugReport,
  terseExpertSql,
  supportAgentSystem,
  multiTurnDebugging,
  toolResultJsonPayload,
  toolResultLogWall,
  codeReviewPaste,
  casualOneLiner,
  marketingProse,
  migrationPlanningWithCache,
  nonNativeDevOps,
  apiDesignDiscussion,
];
