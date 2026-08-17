/**
 * Recorded Anthropic `/v1/messages` request shapes. Each is a wire body that
 * must survive `fromIR(toIR(body))` unchanged.
 */
export type RequestFixture = {
  name: string;
  body: Record<string, unknown>;
};

const plainText: RequestFixture = {
  name: 'plain text conversation',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [{ type: 'text', text: 'What is the capital of France?' }],
      },
      { role: 'assistant', content: [{ type: 'text', text: 'Paris.' }] },
      { role: 'user', content: [{ type: 'text', text: 'And of Japan?' }] },
    ],
  },
};

const stringSystem: RequestFixture = {
  name: 'string-form system',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 512,
    system: 'You are a terse assistant. Answer in one sentence.',
    messages: [{ role: 'user', content: [{ type: 'text', text: 'Hello.' }] }],
  },
};

const arraySystemWithCacheControl: RequestFixture = {
  name: 'array-form system with cache_control',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 2048,
    system: [
      { type: 'text', text: 'You are a coding assistant.' },
      {
        type: 'text',
        text: 'Here is the entire style guide, which is long and stable.',
        cache_control: { type: 'ephemeral' },
      },
    ],
    messages: [{ role: 'user', content: [{ type: 'text', text: 'Review this.' }] }],
  },
};

const stringMessageContent: RequestFixture = {
  name: 'string-form message content',
  body: {
    model: 'claude-haiku-4-5',
    max_tokens: 256,
    messages: [
      { role: 'user', content: 'Just a bare string.' },
      { role: 'assistant', content: 'A bare string reply.' },
      { role: 'user', content: [{ type: 'text', text: 'Now a block array.' }] },
    ],
  },
};

const multiBlockContent: RequestFixture = {
  name: 'multi-block message content',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          { type: 'text', text: 'First paragraph of the question.' },
          { type: 'text', text: 'Second paragraph, with more detail.' },
          {
            type: 'text',
            text: 'Third block carrying a cache breakpoint.',
            cache_control: { type: 'ephemeral' },
          },
        ],
      },
    ],
  },
};

const toolUseAndResult: RequestFixture = {
  name: 'tool_use with matching tool_result',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    tools: [
      {
        name: 'get_weather',
        description: 'Look up the current weather for a city.',
        input_schema: {
          type: 'object',
          properties: { city: { type: 'string' }, unit: { type: 'string' } },
          required: ['city'],
        },
      },
    ],
    messages: [
      { role: 'user', content: [{ type: 'text', text: 'Weather in Oslo?' }] },
      {
        role: 'assistant',
        content: [
          { type: 'text', text: 'Let me check.' },
          {
            type: 'tool_use',
            id: 'toolu_01A',
            name: 'get_weather',
            input: {
              city: 'Oslo',
              unit: 'celsius',
              nested: { deep: [1, 2, null, true] },
            },
          },
        ],
      },
      {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: 'toolu_01A',
            content: '4 degrees, raining',
          },
        ],
      },
    ],
  },
};

const toolResultArrayAndError: RequestFixture = {
  name: 'tool_result with array content and is_error',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'tool_result',
            tool_use_id: 'toolu_02B',
            content: [
              { type: 'text', text: 'Partial output before the failure.' },
              {
                type: 'image',
                source: { type: 'base64', media_type: 'image/png', data: 'iVBORw0KGgo=' },
              },
            ],
            is_error: true,
            cache_control: { type: 'ephemeral' },
          },
          {
            type: 'tool_result',
            tool_use_id: 'toolu_03C',
            content: [{ type: 'text', text: 'A successful result.' }],
            is_error: false,
          },
        ],
      },
    ],
  },
};

const thinkingBlocks: RequestFixture = {
  name: 'thinking and redacted_thinking blocks',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 4096,
    thinking: { type: 'enabled', budget_tokens: 2048 },
    messages: [
      { role: 'user', content: [{ type: 'text', text: 'Solve this puzzle.' }] },
      {
        role: 'assistant',
        content: [
          {
            type: 'thinking',
            thinking: 'Step one, consider the constraints. Step two, eliminate.',
            signature: 'ErUBCkYIBRgCIkDJk9as+signature+bytes+that+must+not+change==',
          },
          { type: 'redacted_thinking', data: 'EroBCkYIBRgCKkBredactedpayload==' },
          { type: 'text', text: 'The answer is 42.' },
        ],
      },
    ],
  },
};

const imageBlocks: RequestFixture = {
  name: 'image blocks with base64 and url sources',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          { type: 'text', text: 'Compare these two images.' },
          {
            type: 'image',
            source: {
              type: 'base64',
              media_type: 'image/jpeg',
              data: '/9j/4AAQSkZJRg==',
            },
          },
          {
            type: 'image',
            source: { type: 'url', url: 'https://example.com/diagram.png' },
            cache_control: { type: 'ephemeral' },
          },
        ],
      },
    ],
  },
};

const documentBlock: RequestFixture = {
  name: 'document block',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          {
            type: 'document',
            source: {
              type: 'base64',
              media_type: 'application/pdf',
              data: 'JVBERi0xLjQKJeLjz9M=',
            },
            title: 'Q3 report',
            context: 'Financials for the quarter.',
            citations: { enabled: true },
          },
          { type: 'text', text: 'Summarize the document.' },
        ],
      },
    ],
  },
};

const unknownBlockType: RequestFixture = {
  name: 'unknown future block type',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    messages: [
      {
        role: 'user',
        content: [
          { type: 'quantum_foo', bar: 1 },
          { type: 'text', text: 'Explain the block above.' },
          {
            type: 'text',
            text: 'Known block, unknown key.',
            future_block_knob: 'keep me',
          },
        ],
      },
    ],
  },
};

const unknownTopLevelField: RequestFixture = {
  name: 'unknown top-level field',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    future_knob: true,
    messages: [{ role: 'user', content: [{ type: 'text', text: 'Hi.' }] }],
  },
};

const allSamplingParams: RequestFixture = {
  name: 'all optional sampling params',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 1024,
    temperature: 0.7,
    top_p: 0.95,
    top_k: 40,
    stop_sequences: ['\n\nHuman:', 'END'],
    stream: false,
    service_tier: 'auto',
    metadata: { user_id: 'user_abc123' },
    messages: [
      { role: 'user', content: [{ type: 'text', text: 'Generate something.' }] },
    ],
  },
};

const serverAndCustomTools: RequestFixture = {
  name: 'custom tools plus a server tool',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 2048,
    tools: [
      {
        name: 'run_query',
        description: 'Run a read-only SQL query.',
        input_schema: {
          type: 'object',
          properties: { sql: { type: 'string' } },
          required: ['sql'],
        },
        cache_control: { type: 'ephemeral' },
      },
      { type: 'web_search_20250305', name: 'web_search', max_uses: 5 },
      { type: 'text_editor_20250124', name: 'str_replace_editor' },
    ],
    tool_choice: { type: 'auto', disable_parallel_tool_use: false },
    mcp_servers: [
      {
        type: 'url',
        url: 'https://mcp.example.com/sse',
        name: 'example',
        authorization_token: 'tok_redacted',
      },
    ],
    container: 'container_xyz',
    messages: [{ role: 'user', content: [{ type: 'text', text: 'Search and query.' }] }],
  },
};

const emptyToolsArray: RequestFixture = {
  name: 'empty tools array',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 512,
    tools: [],
    messages: [{ role: 'user', content: [{ type: 'text', text: 'No tools here.' }] }],
  },
};

const messageLevelUnknownKey: RequestFixture = {
  name: 'unknown key on a message object',
  body: {
    model: 'claude-sonnet-4-5',
    max_tokens: 512,
    messages: [
      { role: 'user', content: 'Hello.', future_message_field: { nested: 'value' } },
    ],
  },
};

export const REQUEST_FIXTURES: readonly RequestFixture[] = [
  plainText,
  stringSystem,
  arraySystemWithCacheControl,
  stringMessageContent,
  multiBlockContent,
  toolUseAndResult,
  toolResultArrayAndError,
  thinkingBlocks,
  imageBlocks,
  documentBlock,
  unknownBlockType,
  unknownTopLevelField,
  allSamplingParams,
  serverAndCustomTools,
  emptyToolsArray,
  messageLevelUnknownKey,
];
