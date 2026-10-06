// The project memory view in the /info panel. Memory names, types, and bodies
// are the project's own data and are shown as written.
export const memoryMessages = {
  en: {
    'memory.unavailable': 'cannot be read: {reason}',
    'memory.count': { one: '{count} memory', other: '{count} memories' },
    'memory.meta': '{count} · index {chars}/{budget} characters sent on every call',
    'memory.skipped': '{file} is skipped and never loaded: {reason}',
    'memory.empty': 'Nothing is remembered for this project yet. The agent writes a memory when it decides something is worth carrying into later sessions.',
    'memory.list': 'Memories',
    'memory.viewer': 'Memory',
    'memory.select': 'Select a memory to read it.',
    'memory.verified': '{written} · verified {verified}',
  },
  'zh-CN': {
    'memory.unavailable': '无法读取：{reason}',
    'memory.count': '{count} 条记忆',
    'memory.meta': '{count} · 索引 {chars}/{budget} 个字符，每次调用都会发送',
    'memory.skipped': '{file} 已跳过，不会被加载：{reason}',
    'memory.empty': '此项目还没有记忆。Agent 认为某些内容值得带到后续会话时，会写入一条记忆。',
    'memory.list': '记忆',
    'memory.viewer': '记忆',
    'memory.select': '选择一条记忆以查看。',
    'memory.verified': '{written} · 已验证 {verified}',
  },
};
